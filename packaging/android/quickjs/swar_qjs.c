// A minimal QuickJS-ng command line for yt-dlp on Android.
//
// yt-dlp solves YouTube's JavaScript challenges with an external runtime;
// without one most videos have no playable formats. Android has none, so the
// app ships this program (as libqjs.so, so the APK installs it executable).
// It does only what yt-dlp's QuickJS provider uses:
//
//   qjs --help            prints "QuickJS-ng version X" (yt-dlp's probe)
//   qjs --script FILE     runs FILE as a classic script; console.log and
//                         print write to stdout, console.error/warn and
//                         uncaught errors to stderr (yt-dlp treats any
//                         stderr output as a failure)
//
// Built from the QuickJS-ng engine sources by scripts/build_quickjs_android.sh.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "quickjs.h"

// The challenge solver parses YouTube's whole player script, which recurses
// deeply; the default 1 MB JS stack is too small for it. The process main
// thread has 8 MB on Android.
#define STACK_SIZE (4 * 1024 * 1024)

static JSValue write_args(JSContext *ctx, FILE *out, int argc, JSValueConst *argv) {
    for (int i = 0; i < argc; i++) {
        const char *s = JS_ToCString(ctx, argv[i]);
        if (!s) return JS_EXCEPTION;
        if (i) fputc(' ', out);
        fputs(s, out);
        JS_FreeCString(ctx, s);
    }
    fputc('\n', out);
    return JS_UNDEFINED;
}

static JSValue js_log(JSContext *ctx, JSValueConst this_val, int argc, JSValueConst *argv) {
    return write_args(ctx, stdout, argc, argv);
}

static JSValue js_error(JSContext *ctx, JSValueConst this_val, int argc, JSValueConst *argv) {
    return write_args(ctx, stderr, argc, argv);
}

static void report_exception(JSContext *ctx) {
    JSValue exc = JS_GetException(ctx);
    const char *msg = JS_ToCString(ctx, exc);
    fprintf(stderr, "%s\n", msg ? msg : "uncaught exception");
    if (msg) JS_FreeCString(ctx, msg);
    if (JS_IsObject(exc)) {
        JSValue stack = JS_GetPropertyStr(ctx, exc, "stack");
        if (JS_IsString(stack)) {
            const char *s = JS_ToCString(ctx, stack);
            if (s) {
                fprintf(stderr, "%s\n", s);
                JS_FreeCString(ctx, s);
            }
        }
        JS_FreeValue(ctx, stack);
    }
    JS_FreeValue(ctx, exc);
}

static char *read_file(const char *path, size_t *len) {
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;
    char *buf = NULL;
    size_t cap = 0, n = 0;
    for (;;) {
        if (n + 65536 + 1 > cap) {
            cap = (cap ? cap * 2 : 1 << 20);
            char *grown = realloc(buf, cap);
            if (!grown) {
                free(buf);
                fclose(f);
                return NULL;
            }
            buf = grown;
        }
        size_t got = fread(buf + n, 1, cap - n - 1, f);
        n += got;
        if (got == 0) break;
    }
    int failed = ferror(f);
    fclose(f);
    if (failed) {
        free(buf);
        return NULL;
    }
    buf[n] = '\0';
    *len = n;
    return buf;
}

static int usage(void) {
    // yt-dlp reads the version from this first line; --help exiting with 1
    // matches upstream qjs, and yt-dlp ignores the status.
    printf("QuickJS-ng version %s\nusage: qjs --script FILE\n", JS_GetVersion());
    return 1;
}

int main(int argc, char **argv) {
    const char *path = NULL;
    if (argc == 3 && strcmp(argv[1], "--script") == 0) path = argv[2];
    else if (argc == 2 && strcmp(argv[1], "--") != 0 && argv[1][0] != '-') path = argv[1];
    if (!path) return usage();

    size_t len = 0;
    char *src = read_file(path, &len);
    if (!src) {
        fprintf(stderr, "qjs: cannot read %s\n", path);
        return 1;
    }

    JSRuntime *rt = JS_NewRuntime();
    JSContext *ctx = rt ? JS_NewContext(rt) : NULL;
    if (!ctx) {
        fprintf(stderr, "qjs: out of memory\n");
        return 1;
    }
    JS_SetMaxStackSize(rt, STACK_SIZE);

    JSValue global = JS_GetGlobalObject(ctx);
    JSValue console = JS_NewObject(ctx);
    JS_SetPropertyStr(ctx, console, "log", JS_NewCFunction(ctx, js_log, "log", 1));
    JS_SetPropertyStr(ctx, console, "info", JS_NewCFunction(ctx, js_log, "info", 1));
    JS_SetPropertyStr(ctx, console, "debug", JS_NewCFunction(ctx, js_log, "debug", 1));
    JS_SetPropertyStr(ctx, console, "warn", JS_NewCFunction(ctx, js_error, "warn", 1));
    JS_SetPropertyStr(ctx, console, "error", JS_NewCFunction(ctx, js_error, "error", 1));
    JS_SetPropertyStr(ctx, global, "console", console);
    JS_SetPropertyStr(ctx, global, "print", JS_NewCFunction(ctx, js_log, "print", 1));
    JS_FreeValue(ctx, global);

    int status = 0;
    JSValue result = JS_Eval(ctx, src, len, path, JS_EVAL_TYPE_GLOBAL);
    free(src);
    if (JS_IsException(result)) {
        report_exception(ctx);
        status = 1;
    }
    JS_FreeValue(ctx, result);

    // Promise jobs the script queued.
    while (status == 0) {
        JSContext *job_ctx;
        int r = JS_ExecutePendingJob(rt, &job_ctx);
        if (r == 0) break;
        if (r < 0) {
            report_exception(job_ctx);
            status = 1;
        }
    }

    fflush(stdout);
    JS_FreeContext(ctx);
    JS_FreeRuntime(rt);
    return status;
}
