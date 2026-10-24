package com.sonivo.sonivo

import android.content.Context
import android.os.Handler
import android.os.Looper
import com.yausername.youtubedl_android.YoutubeDL
import com.yausername.youtubedl_android.YoutubeDLRequest
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * Runs yt-dlp on the phone for Swar's Deep Search and playback.
 *
 * Swar ships without yt-dlp: the app downloads the official yt-dlp zipapp,
 * checks its SHA-256 against the release, and calls [activate] with it.
 * This copies it to where youtubedl-android runs yt-dlp from (checking the
 * hash again), and the library's bundled Python runs it. The library only
 * copies its own bundled yt-dlp when that file is missing, and Swar never
 * initialises the library before a verified copy is in place, so the
 * bundled one (stripped from release builds) is never used.
 *
 * Every run happens off the main thread, at most two at a time, with a
 * timeout and an output limit; Dart can cancel a run by its id.
 *
 * YouTube hides most audio formats behind JavaScript challenges, which
 * yt-dlp solves with an external runtime. The APK ships QuickJS-ng as
 * libqjs.so (scripts/build_quickjs_android.sh) and every run is pointed at
 * it. Runs share a cache folder, so YouTube's player script is downloaded
 * and prepared once instead of on every song.
 */
object YtDlpChannel : MethodChannel.MethodCallHandler {
    private const val CHANNEL = "swar/ytdlp"

    private lateinit var app: Context
    private var channel: MethodChannel? = null
    private val main = Handler(Looper.getMainLooper())

    // Python is heavy on a phone: two runs at once at most.
    private val pool = Executors.newFixedThreadPool(2)
    private val watchdog = Executors.newSingleThreadScheduledExecutor()
    private val running = ConcurrentHashMap.newKeySet<String>()
    private val timedOut = ConcurrentHashMap.newKeySet<String>()

    @Volatile
    private var ready = false

    /** Registers on an engine. Safe to call again for a new activity. */
    fun register(context: Context, messenger: BinaryMessenger) {
        app = context.applicationContext
        channel?.setMethodCallHandler(null)
        channel = MethodChannel(messenger, CHANNEL).also { it.setMethodCallHandler(this) }
    }

    /**
     * Where youtubedl-android runs yt-dlp from: its baseName, ytdlpDirName
     * and ytdlpBin ("youtubedl-android", "yt-dlp", "yt-dlp"). Check these
     * when upgrading the library.
     */
    private fun deployed(): File =
        File(File(File(app.noBackupFilesDir, "youtubedl-android"), "yt-dlp"), "yt-dlp")

    /** The bundled QuickJS-ng, when this APK has one. */
    private val quickJs: File? by lazy {
        File(app.applicationInfo.nativeLibraryDir, "libqjs.so").takeIf { it.canExecute() }
    }

    /** yt-dlp's cache (player scripts, solved challenges). */
    private val cacheDir: File by lazy { File(app.cacheDir, "yt-dlp").also { it.mkdirs() } }

    @Synchronized
    private fun ensureReady() {
        if (ready) return
        if (!deployed().exists()) throw IllegalStateException("not_installed")
        YoutubeDL.getInstance().init(app)
        ready = true
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "installed" -> result.success(deployed().exists())
            "activate" -> pool.execute { reply(result) { activate(call) } }
            // Smoke test after an install: does the deployed yt-dlp start?
            "version" -> pool.execute {
                reply(result) {
                    ensureReady()
                    val request = YoutubeDLRequest(emptyList<String>())
                    request.addOption("--ignore-config")
                    request.addOption("--version")
                    YoutubeDL.getInstance().execute(request, "version", false, null).out.trim().lines().first().take(40)
                }
            }
            "run" -> run(call, result)
            "cancel" -> {
                call.argument<String>("id")?.let { id ->
                    if (running.contains(id)) YoutubeDL.getInstance().destroyProcessById(id)
                }
                result.success(null)
            }
            else -> result.notImplemented()
        }
    }

    /** Copies a verified file into place, atomically, checking it again. */
    private fun activate(call: MethodCall): Boolean {
        val path = call.argument<String>("path") ?: throw IllegalArgumentException("path is required")
        val sha = call.argument<String>("sha256")?.lowercase() ?: throw IllegalArgumentException("sha256 is required")
        val source = File(path)
        // Only files Swar downloaded into its own storage.
        if (!source.canonicalPath.startsWith(app.filesDir.parentFile!!.canonicalPath)) {
            throw IllegalArgumentException("unexpected source")
        }
        val dest = deployed()
        if (dest.exists() && sha256(dest) == sha) return true
        if (sha256(source) != sha) throw IllegalStateException("checksum_mismatch")
        dest.parentFile?.mkdirs()
        val tmp = File(dest.parentFile, "yt-dlp.tmp")
        source.copyTo(tmp, overwrite = true)
        if (sha256(tmp) != sha) {
            tmp.delete()
            throw IllegalStateException("checksum_mismatch")
        }
        if (!tmp.renameTo(dest)) {
            dest.delete()
            if (!tmp.renameTo(dest)) throw IllegalStateException("could not install yt-dlp")
        }
        return true
    }

    private fun run(call: MethodCall, result: MethodChannel.Result) {
        val id = call.argument<String>("id")
        val target = call.argument<String>("target")
        val options = call.argument<List<List<String>>>("options") ?: emptyList()
        val timeoutMs = (call.argument<Number>("timeoutMs") ?: 30000).toLong()
        val maxBytes = (call.argument<Number>("maxBytes") ?: (8 shl 20)).toInt()
        // Same rule as the server: only a search or a canonical watch URL.
        val validTarget = target != null &&
            (target.startsWith("ytsearch") || Regex("^https://www\\.youtube\\.com/watch\\?v=[A-Za-z0-9_-]{11}$").matches(target))
        if (id == null || !validTarget) {
            result.error("bad_args", "unsupported request", null)
            return
        }
        pool.execute {
            running.add(id)
            val timer = watchdog.schedule({
                if (running.contains(id)) {
                    timedOut.add(id)
                    YoutubeDL.getInstance().destroyProcessById(id)
                }
            }, timeoutMs, TimeUnit.MILLISECONDS)
            try {
                ensureReady()
                val request = YoutubeDLRequest(target!!)
                for (o in options) {
                    when (o.size) {
                        1 -> request.addOption(o[0])
                        2 -> request.addOption(o[0], o[1])
                    }
                }
                quickJs?.let { request.addOption("--js-runtimes", "quickjs:${it.absolutePath}") }
                // Without this the library adds --no-cache-dir.
                request.addOption("--cache-dir", cacheDir.absolutePath)
                val response = YoutubeDL.getInstance().execute(request, id, false, null)
                val out = response.out
                if (out.length > maxBytes) post { result.error("too_large", "yt-dlp output exceeded the limit", null) }
                else post { result.success(out) }
            } catch (e: YoutubeDL.CanceledException) {
                val code = if (timedOut.contains(id)) "timeout" else "canceled"
                post { result.error(code, code, null) }
            } catch (e: IllegalStateException) {
                val msg = e.message ?: "failed"
                post { result.error(if (msg == "not_installed") "not_installed" else "failed", msg, null) }
            } catch (e: Exception) {
                // stderr from yt-dlp; Dart turns it into a short reason.
                val msg = (e.message ?: "yt-dlp failed").takeLast(800)
                post { result.error("failed", msg, null) }
            } finally {
                timer.cancel(false)
                running.remove(id)
                timedOut.remove(id)
            }
        }
    }

    private fun <T> reply(result: MethodChannel.Result, block: () -> T) {
        try {
            val v = block()
            post { result.success(v) }
        } catch (e: Exception) {
            post { result.error("failed", e.message ?: "failed", null) }
        }
    }

    private fun post(block: () -> Unit) {
        main.post(block)
    }

    private fun sha256(f: File): String {
        val md = MessageDigest.getInstance("SHA-256")
        f.inputStream().use { input ->
            val buf = ByteArray(64 * 1024)
            while (true) {
                val n = input.read(buf)
                if (n <= 0) break
                md.update(buf, 0, n)
            }
        }
        return md.digest().joinToString("") { "%02x".format(it) }
    }
}
