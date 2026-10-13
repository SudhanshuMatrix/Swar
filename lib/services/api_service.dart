import 'dart:convert';

import 'package:http/http.dart' as http;

import '../core/constants.dart';
import '../models/track.dart';

class StreamResult {
  final String url;
  final Track track;
  StreamResult({required this.url, required this.track});
}

/// Result of resolving Video mode: YouTube rarely serves one combined
/// stream any more, so this carries both the muted video-only [videoUrl]
/// and the real [audioUrl] — the client plays them in sync.
class VideoStreamResult {
  final String videoUrl;
  final String audioUrl;
  final Track track;
  VideoStreamResult({required this.videoUrl, required this.audioUrl, required this.track});
}

/// Talks to the Sonivo backend (/backend), which wraps yt-dlp over HTTP.
class ApiService {
  final String baseUrl;
  ApiService({String? baseUrl}) : baseUrl = baseUrl ?? ApiConfig.baseUrl;

  Future<List<Track>> search(String query, {int limit = 25}) async {
    final uri = Uri.parse('$baseUrl/search').replace(queryParameters: {
      'q': query,
      'limit': '$limit',
    });
    final res = await http.get(uri);
    if (res.statusCode != 200) {
      throw Exception('Search failed (${res.statusCode}): ${res.body}');
    }
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    final results = (data['results'] as List).cast<Map<String, dynamic>>();
    return results.map(Track.fromJson).toList();
  }

  Future<StreamResult> resolveAudio(String videoId) async {
    final res = await http.get(Uri.parse('$baseUrl/track/$videoId'));
    if (res.statusCode != 200) {
      throw Exception('Could not resolve audio (${res.statusCode}): ${res.body}');
    }
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    return StreamResult(url: data['audio_url'] as String, track: Track.fromJson(data));
  }

  Future<VideoStreamResult> resolveVideo(String videoId) async {
    final res = await http.get(Uri.parse('$baseUrl/video/$videoId'));
    if (res.statusCode != 200) {
      throw Exception('Could not resolve video (${res.statusCode}): ${res.body}');
    }
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    return VideoStreamResult(
      videoUrl: data['video_url'] as String,
      audioUrl: data['audio_url'] as String,
      track: Track.fromJson(data),
    );
  }

  Future<List<Track>> getRelated(String videoId) async {
    final res = await http.get(Uri.parse('$baseUrl/related/$videoId'));
    if (res.statusCode != 200) {
      throw Exception('Could not fetch related tracks (${res.statusCode}): ${res.body}');
    }
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    final results = (data['results'] as List).cast<Map<String, dynamic>>();
    return results.map(Track.fromJson).toList();
  }
}
