import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:sonivo/services/ytdlp/ytdlp_input.dart';
import 'package:sonivo/services/ytdlp/ytdlp_installer.dart';
import 'package:sonivo/services/ytdlp/ytdlp_parse.dart';

void main() {
  group('input', () {
    test('searches and YouTube links are accepted, other URLs refused', () {
      expect(YtDlpTarget.parse('  kesariya   arijit ').argument(10), 'ytsearch10:kesariya arijit');
      expect(YtDlpTarget.parse('https://youtu.be/dQw4w9WgXcQ?si=x').argument(10),
          'https://www.youtube.com/watch?v=dQw4w9WgXcQ');
      expect(YtDlpTarget.parse('music.youtube.com/watch?v=dQw4w9WgXcQ').videoId, 'dQw4w9WgXcQ');
      expect(YtDlpTarget.parse('dQw4w9WgXcQ').videoId, 'dQw4w9WgXcQ');
      expect(YtDlpTarget.parse('beautifully').isVideo, isFalse, reason: 'plain words stay searches');
      for (final bad in ['http://169.254.169.254/x', 'https://evil.example/watch?v=dQw4w9WgXcQ', 'file:///etc/passwd',
          'www.example.com', '', 'a', 'x' * 201]) {
        expect(() => YtDlpTarget.parse(bad), throwsA(isA<YtDlpInputException>()), reason: bad);
      }
    });

    test('a query starting with a dash cannot become an option', () {
      final arg = YtDlpTarget.parse('--exec rm -rf /').argument(5);
      expect(arg, 'ytsearch5:--exec rm -rf /');
      expect(arg.startsWith('-'), isFalse);
    });
  });

  group('parse', () {
    test('search results are deduplicated and cleaned', () {
      final tracks = parseSearch({
        'entries': [
          {'id': 'AAAAAAAAAAA', 'title': 'Song A', 'channel': 'Singer VEVO', 'duration': 200.0},
          {'id': 'AAAAAAAAAAA', 'title': 'dup'},
          {'id': 'BBBBBBBBBBB', 'title': 'Live', 'live_status': 'is_live'},
          {'id': 'bad', 'title': 'bad id'},
          {
            'id': 'CCCCCCCCCCC',
            'title': 'Song C',
            'thumbnails': [
              {'url': 'http://insecure/c.jpg', 'width': 480, 'height': 360},
              {'url': 'https://i.ytimg.com/c.jpg', 'width': 480, 'height': 360},
            ]
          },
        ]
      }, 10);
      expect(tracks.map((t) => t.id), ['yt:AAAAAAAAAAA', 'yt:CCCCCCCCCCC']);
      expect(tracks[0].artist, 'Singer');
      expect(tracks[0].duration, const Duration(seconds: 200));
      expect(tracks[0].isDeepSearchResult, isTrue);
      expect(tracks[0].youtubeId, 'AAAAAAAAAAA');
      expect(tracks[1].thumbnailUrl, 'https://i.ytimg.com/c.jpg');
      expect(tracks[0].thumbnailUrl, startsWith('https://i.ytimg.com/vi/AAAAAAAAAAA/'));
    });

    test('resolve prefers m4a, skips DRM and non-https, reads expiry', () {
      final now = DateTime(2026, 10, 24, 12);
      final exp = now.add(const Duration(hours: 2)).millisecondsSinceEpoch ~/ 1000;
      final s = parseResolve({
        'availability': 'public',
        'formats': [
          {'url': 'https://x/drm', 'ext': 'm4a', 'acodec': 'mp4a', 'vcodec': 'none', 'abr': 999, 'protocol': 'https', 'has_drm': true},
          {'url': 'https://x/opus?expire=$exp', 'ext': 'webm', 'acodec': 'opus', 'vcodec': 'none', 'abr': 160, 'protocol': 'https'},
          {
            'url': 'https://x/aac?expire=$exp',
            'ext': 'm4a',
            'acodec': 'mp4a',
            'vcodec': 'none',
            'abr': 128,
            'protocol': 'https',
            'http_headers': {'User-Agent': 'UA', 'Cookie': 'never-forwarded'}
          },
          {'url': 'https://x/hls', 'ext': 'm4a', 'acodec': 'mp4a', 'vcodec': 'none', 'abr': 500, 'protocol': 'm3u8_native'},
          {'url': 'http://x/plain', 'ext': 'm4a', 'acodec': 'mp4a', 'vcodec': 'none', 'abr': 400, 'protocol': 'http'},
          {'url': 'https://x/video', 'ext': 'mp4', 'acodec': 'none', 'vcodec': 'avc1', 'protocol': 'https'},
        ]
      }, now: now);
      expect(s.url, startsWith('https://x/aac'));
      expect(s.headers, {'User-Agent': 'UA'});
      expect(s.expires, now.add(const Duration(hours: 1, minutes: 50)));
      expect(s.via, 'yt-dlp');
    });

    test('restricted, live and audio-less videos are refused', () {
      for (final root in [
        {'availability': 'subscriber_only', 'formats': []},
        {'availability': 'needs_auth', 'formats': []},
        {'live_status': 'is_live', 'formats': []},
        {'availability': 'public', 'formats': []},
      ]) {
        expect(() => parseResolve(root), throwsA(isA<YtDlpUnplayable>()), reason: '$root');
      }
    });

    test('expiry falls back and is bounded', () {
      final now = DateTime(2026, 1, 1);
      expect(streamExpiry('https://x/a', now: now), now.add(const Duration(minutes: 30)));
      final past = now.subtract(const Duration(hours: 1)).millisecondsSinceEpoch ~/ 1000;
      expect(streamExpiry('https://x/a?expire=$past', now: now), now);
      final far = now.add(const Duration(days: 3)).millisecondsSinceEpoch ~/ 1000;
      expect(streamExpiry('https://x/a?expire=$far', now: now), now.add(const Duration(minutes: 30)));
    });

    test('failures get a short, safe reason', () {
      expect(explainFailure('ERROR: [youtube] x: Sign in to confirm you’re not a bot'), contains('sign in'));
      expect(explainFailure('ERROR: Private video'), contains('private'));
      expect(explainFailure('Traceback /home/user/secret/path.py'), isNot(contains('/home')));
    });
  });

  group('installer', () {
    test('asset per platform', () {
      expect(YtDlpInstaller.assetName(os: 'linux', arch: 'x64'), 'yt-dlp_linux');
      expect(YtDlpInstaller.assetName(os: 'linux', arch: 'arm64'), 'yt-dlp_linux_aarch64');
      expect(YtDlpInstaller.assetName(os: 'windows', arch: 'x64'), 'yt-dlp.exe');
      expect(YtDlpInstaller.assetName(os: 'windows', arch: 'arm64'), 'yt-dlp_arm64.exe');
      expect(YtDlpInstaller.assetName(os: 'macos', arch: 'arm64'), 'yt-dlp_macos');
      expect(YtDlpInstaller.assetName(os: 'android', arch: 'arm64'), 'yt-dlp');
      expect(YtDlpInstaller.assetName(os: 'fuchsia', arch: 'x64'), isNull);
    });

    test('checksum list parsing', () {
      final a = 'a' * 64, b = 'B' * 64;
      final sums = YtDlpInstaller.parseSums('$a  yt-dlp\n$b *yt-dlp.exe\nnot a line\n');
      expect(sums, {'yt-dlp': a, 'yt-dlp.exe': 'b' * 64});
    });

    late Directory dir;
    setUp(() => dir = Directory.systemTemp.createTempSync('swar-ytdlp'));
    tearDown(() => dir.deleteSync(recursive: true));

    /// A fake release server serving whatever [binary] returns now.
    MockClient release(List<int> Function() binary, {String? sha}) => MockClient((req) async {
          final name = req.url.pathSegments.last;
          if (name == 'SHA2-256SUMS') {
            return http.Response('${sha ?? sha256.convert(binary())}  yt-dlp\n', 200);
          }
          if (name == 'yt-dlp') return http.Response.bytes(binary(), 200);
          return http.Response('', 404);
        });

    test('a download that does not match its checksum is never run', () async {
      var ran = false;
      final i = YtDlpInstaller(
        dir: dir,
        os: 'android',
        client: release(() => utf8.encode('evil'), sha: 'f' * 64),
        smokeTest: (_) async {
          ran = true;
          return '2026.10.01';
        },
      );
      await expectLater(i.installLatest(), throwsA(isA<YtDlpInstallException>()));
      expect(ran, isFalse);
      expect(await i.verifiedPath(), isNull);
      expect(Directory('${dir.path}/bin').listSync(), isEmpty, reason: 'the bad file is deleted');
    });

    test('install, deploy, no re-download when current, update, cleanup, rollback', () async {
      final deployed = <String>[];
      var version = '2026.10.01';
      var binary = utf8.encode('v1');
      YtDlpInstaller make() => YtDlpInstaller(
            dir: dir,
            os: 'android',
            client: release(() => binary),
            smokeTest: (_) async => version,
            deploy: (path, sha) async => deployed.add(sha),
          );
      final i = make();
      final progress = <double?>[];
      expect(await i.installLatest(onProgress: progress.add), '2026.10.01');
      expect(progress.last, 1.0);
      final first = await i.verifiedPath();
      expect(first, isNotNull);
      expect(await i.lastSuccessfulCheck(), isNotNull);
      expect(deployed.single, sha256.convert(binary).toString());

      // Same release: nothing is downloaded again.
      expect(await i.installLatest(), '2026.10.01');
      expect(await i.verifiedPath(), first);

      // New release replaces it; the old one stays until a real run.
      binary = utf8.encode('v2');
      version = '2026.10.20';
      expect(await i.installLatest(), '2026.10.20');
      expect(Directory('${dir.path}/bin').listSync().length, 2);
      expect(await i.rollback(), isTrue);
      expect(await i.verifiedPath(), first);
      expect(await i.rollback(), isTrue); // and forward again
      await i.confirmWorking();
      expect(Directory('${dir.path}/bin').listSync().length, 1, reason: 'old version cleaned up');
      expect(await i.rollback(), isFalse);

      // A file changed on disk is not trusted.
      final current = File((await make().verifiedPath())!);
      current.writeAsStringSync('tampered');
      expect(await make().verifiedPath(), isNull);
    });

    test('a new version that does not start is discarded and the old one redeployed', () async {
      final deployed = <String>[];
      String? version = '2026.10.01';
      var binary = utf8.encode('v1');
      YtDlpInstaller make() => YtDlpInstaller(
            dir: dir,
            os: 'android',
            client: release(() => binary),
            smokeTest: (_) async => version,
            deploy: (path, sha) async => deployed.add(sha),
          );
      await make().installLatest();
      final good = deployed.last;
      binary = utf8.encode('broken');
      version = null;
      await expectLater(make().installLatest(), throwsA(isA<YtDlpInstallException>()));
      expect(deployed.last, good);
      expect(await make().installedVersion(), '2026.10.01');
    });

    test('cleanUp removes partial downloads', () async {
      Directory('${dir.path}/bin').createSync(recursive: true);
      final partial = File('${dir.path}/bin/download.tmp')..writeAsStringSync('half');
      await YtDlpInstaller(dir: dir, os: 'android').cleanUp();
      expect(partial.existsSync(), isFalse);
    });
  });
}
