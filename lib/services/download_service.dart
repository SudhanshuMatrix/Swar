import 'dart:async';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:hive_flutter/hive_flutter.dart';
import 'package:http/http.dart' as http;
import 'package:path_provider/path_provider.dart';

import '../models/track.dart';
import 'api_service.dart';

enum DownloadStatus { queued, downloading, paused, verifying, completed, failed, canceled }

class DownloadItem {
  final Track track;
  DownloadStatus status;
  int bytesDone;
  int bytesTotal;
  String path;
  String sha256;
  String error;

  DownloadItem({
    required this.track,
    required this.status,
    this.bytesDone = 0,
    this.bytesTotal = 0,
    this.path = '',
    this.sha256 = '',
    this.error = '',
  });

  double get progress => bytesTotal > 0 ? (bytesDone / bytesTotal).clamp(0.0, 1.0) : 0;

  Map<String, dynamic> toMap() => {
        'track': track.toMap(),
        'status': status.name,
        'bytesDone': bytesDone,
        'bytesTotal': bytesTotal,
        'path': path,
        'sha256': sha256,
        'error': error,
      };

  factory DownloadItem.fromMap(Map<dynamic, dynamic> m) => DownloadItem(
        track: Track.fromMap(Map<dynamic, dynamic>.from(m['track'] as Map)),
        status: DownloadStatus.values.firstWhere((s) => s.name == m['status'], orElse: () => DownloadStatus.failed),
        bytesDone: (m['bytesDone'] as int?) ?? 0,
        bytesTotal: (m['bytesTotal'] as int?) ?? 0,
        path: (m['path'] as String?) ?? '',
        sha256: (m['sha256'] as String?) ?? '',
        error: (m['error'] as String?) ?? '',
      );
}

/// Offline downloads.
///
/// Audio is fetched by the app directly from the authorized source the
/// server names (the server never relays audio). Files live in the app's
/// private support directory, are resumed with HTTP Range requests, and are
/// verified (size + SHA-256) before they count as complete. Track metadata
/// is kept in Hive, separate from the audio files, so clearing one never
/// silently corrupts the other.
class DownloadService extends ChangeNotifier {
  final ApiService api;
  DownloadService({required this.api});

  static const _boxName = 'downloads';
  Box? _box;
  final Map<String, DownloadItem> _items = {};
  final Map<String, _Job> _jobs = {};

  List<DownloadItem> get items {
    final list = _items.values.toList();
    list.sort((a, b) => a.track.title.toLowerCase().compareTo(b.track.title.toLowerCase()));
    return list;
  }

  DownloadItem? item(String trackId) => _items[trackId];

  bool get supported => !kIsWeb;

  DownloadStatus? statusOf(String trackId) => _items[trackId]?.status;
  double progressOf(String trackId) => _items[trackId]?.progress ?? 0;

  Future<void> init() async {
    if (!supported) return;
    _box = await Hive.openBox(_boxName);
    for (final key in _box!.keys) {
      final item = DownloadItem.fromMap(Map<dynamic, dynamic>.from(_box!.get(key) as Map));
      // A download that was running when the app closed is now paused.
      if (item.status == DownloadStatus.downloading || item.status == DownloadStatus.queued) {
        item.status = DownloadStatus.paused;
      }
      if (item.status == DownloadStatus.verifying) {
        item.status = DownloadStatus.failed;
        item.error = 'Interrupted while verifying';
      }
      // A completed file that disappeared (storage cleared) is not offline.
      if (item.status == DownloadStatus.completed && !File(item.path).existsSync()) {
        item.status = DownloadStatus.failed;
        item.error = 'File missing; retry to download again';
        unawaited(_report(item));
      }
      _items[item.track.id] = item;
    }
    notifyListeners();
  }

  /// The local file to play instead of streaming, if this track is fully
  /// downloaded and still on disk.
  String? localFileFor(String trackId) {
    final item = _items[trackId];
    if (item == null || item.status != DownloadStatus.completed) return null;
    return File(item.path).existsSync() ? item.path : null;
  }

  int get bytesUsed => _items.values.fold(0, (sum, i) => sum + (i.status == DownloadStatus.completed ? i.bytesTotal : i.bytesDone));

  int get completedCount => _items.values.where((i) => i.status == DownloadStatus.completed).length;

  /// Starts (or retries) a download. Throws [ApiException] with the
  /// server's reason when the track is not eligible for offline use.
  Future<void> download(Track track) async {
    if (!supported) throw ApiException(400, 'unsupported', 'Offline downloads are not available on the web');
    final existing = _items[track.id];
    if (existing != null && (existing.status == DownloadStatus.completed || _jobs.containsKey(track.id))) return;
    final eligibility = await api.downloadEligibility(track.id);
    if (!eligibility.eligible) {
      throw ApiException(403, 'unavailable', _reasonText(eligibility.reason));
    }
    final dir = Directory('${(await getApplicationSupportDirectory()).path}/downloads');
    await dir.create(recursive: true);
    final item = existing ?? DownloadItem(track: track, status: DownloadStatus.queued);
    item
      ..status = DownloadStatus.queued
      ..error = ''
      ..path = '${dir.path}/${eligibility.localId}.audio';
    if (existing == null || existing.status != DownloadStatus.paused) {
      // A fresh start or a retry after failure starts from zero.
      item.bytesDone = 0;
      final f = File(item.path);
      if (await f.exists()) await f.delete();
    }
    _items[track.id] = item;
    await _save(item);
    await _report(item);
    _run(item, eligibility.url!);
  }

  Future<void> resume(String trackId) async {
    final item = _items[trackId];
    if (item == null || item.status != DownloadStatus.paused) return;
    // Stream URLs expire, so ask for a fresh one each time.
    final eligibility = await api.downloadEligibility(trackId);
    if (!eligibility.eligible) {
      item
        ..status = DownloadStatus.canceled
        ..error = _reasonText(eligibility.reason);
      await _save(item);
      await _report(item);
      notifyListeners();
      return;
    }
    _run(item, eligibility.url!);
  }

  Future<void> pause(String trackId) async {
    final job = _jobs.remove(trackId);
    final item = _items[trackId];
    if (job == null || item == null) return;
    await job.cancel();
    item.status = DownloadStatus.paused;
    await _save(item);
    await _report(item);
    notifyListeners();
  }

  Future<void> cancel(String trackId) async {
    await _jobs.remove(trackId)?.cancel();
    final item = _items[trackId];
    if (item == null) return;
    item
      ..status = DownloadStatus.canceled
      ..bytesDone = 0;
    await _deleteFile(item);
    await _save(item);
    await _report(item);
    notifyListeners();
  }

  /// Deletes the file and forgets the download entirely.
  Future<void> remove(String trackId) async {
    await _jobs.remove(trackId)?.cancel();
    final item = _items.remove(trackId);
    if (item == null) return;
    await _deleteFile(item);
    await _box?.delete(trackId);
    try {
      await api.removeDownloadRecord(trackId);
    } catch (_) {}
    notifyListeners();
  }

  void _run(DownloadItem item, String url) {
    final job = _Job();
    _jobs[item.track.id] = job;
    unawaited(_transfer(item, url, job));
  }

  Future<void> _transfer(DownloadItem item, String url, _Job job) async {
    final file = File(item.path);
    var offset = await file.exists() ? await file.length() : 0;
    try {
      final req = http.Request('GET', Uri.parse(url));
      if (offset > 0) req.headers['Range'] = 'bytes=$offset-';
      final res = await job.client.send(req);
      if (res.statusCode == 200 && offset > 0) {
        // The source ignored the Range header: start over.
        offset = 0;
      } else if (res.statusCode != 200 && res.statusCode != 206) {
        throw HttpException('Source answered ${res.statusCode}');
      }
      final total = res.statusCode == 206 ? _totalFromContentRange(res.headers['content-range']) : res.contentLength;
      item
        ..status = DownloadStatus.downloading
        ..bytesDone = offset
        ..bytesTotal = total ?? 0;
      await _save(item);
      await _report(item);
      notifyListeners();

      final sink = file.openWrite(mode: offset > 0 ? FileMode.append : FileMode.write);
      var lastNotify = DateTime.now();
      try {
        await for (final chunk in res.stream) {
          if (job.canceled) break;
          sink.add(chunk);
          item.bytesDone += chunk.length;
          if (DateTime.now().difference(lastNotify) > const Duration(milliseconds: 250)) {
            lastNotify = DateTime.now();
            notifyListeners();
          }
        }
      } finally {
        await sink.flush();
        await sink.close();
      }
      if (job.canceled) return;
      if (item.bytesTotal == 0) item.bytesTotal = item.bytesDone;

      item.status = DownloadStatus.verifying;
      await _report(item);
      notifyListeners();
      final size = await file.length();
      if (size != item.bytesTotal || size == 0) {
        throw const FileSystemException('Downloaded size does not match');
      }
      item.sha256 = (await sha256.bind(file.openRead()).first).toString();
      item.status = DownloadStatus.completed;
      await _save(item);
      await _report(item);
    } catch (e) {
      if (job.canceled) return;
      item
        ..status = DownloadStatus.failed
        ..error = e is ApiException ? e.message : 'Download failed: $e';
      await _save(item);
      await _report(item);
    } finally {
      job.client.close();
      if (_jobs[item.track.id] == job) _jobs.remove(item.track.id);
      notifyListeners();
    }
  }

  int? _totalFromContentRange(String? header) {
    // "bytes 100-999/1000"
    final slash = header?.lastIndexOf('/') ?? -1;
    if (slash < 0) return null;
    return int.tryParse(header!.substring(slash + 1));
  }

  Future<void> _deleteFile(DownloadItem item) async {
    if (item.path.isEmpty) return;
    final f = File(item.path);
    if (await f.exists()) await f.delete();
  }

  Future<void> _save(DownloadItem item) async => _box?.put(item.track.id, item.toMap());

  /// Best effort: the local state is authoritative for the files; the
  /// server record lets other features know what is offline.
  Future<void> _report(DownloadItem item) async {
    try {
      await api.reportDownload(
        item.track.id,
        item.status.name,
        bytesTotal: item.bytesTotal,
        bytesDone: item.bytesDone,
        sha256: item.status == DownloadStatus.completed ? item.sha256 : '',
        error: item.error,
      );
    } catch (_) {}
  }

  static String _reasonText(String reason) {
    switch (reason) {
      case 'downloads_not_enabled_for_source':
        return 'Offline downloads are not enabled for this source on your Swar server.';
      case 'provider_rights_disallow':
        return 'The source does not allow offline copies of this song.';
      case 'no_media_reference':
      case 'source_playback_not_enabled':
        return 'This song has no downloadable source.';
      default:
        return 'This song cannot be downloaded${reason.isEmpty ? '' : ' ($reason)'}.';
    }
  }
}

class _Job {
  final http.Client client = http.Client();
  bool canceled = false;

  Future<void> cancel() async {
    canceled = true;
    client.close();
  }
}
