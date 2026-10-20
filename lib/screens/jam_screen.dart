import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:provider/provider.dart';

import '../core/errors.dart';
import '../core/jam_validation.dart';
import '../core/theme.dart';
import '../services/jam_service.dart';
import '../widgets/track_tile.dart';

/// Swar Jam: listen together. Everyone plays the same queue on their own
/// device, in sync; only the admin controls playback.
class JamScreen extends StatelessWidget {
  const JamScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final jam = context.watch<JamController>();
    return SafeArea(
      bottom: false,
      child: Align(
        alignment: Alignment.topCenter,
        // Forms read best in a narrower column on wide windows.
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: jam.jamId == null ? const _JamLobby() : const _JamRoom(),
        ),
      ),
    );
  }
}

class _JamLobby extends StatefulWidget {
  const _JamLobby();

  @override
  State<_JamLobby> createState() => _JamLobbyState();
}

class _JamLobbyState extends State<_JamLobby> {
  final _name = TextEditingController();
  final _code = TextEditingController();
  bool _busy = false;
  String? _error;
  String? _nameError;
  String? _codeError;

  @override
  void dispose() {
    _name.dispose();
    _code.dispose();
    super.dispose();
  }

  /// Checks the form; on a mistake marks the field and shows a popup.
  bool _valid({required bool needCode}) {
    final nameError = validateJamName(_name.text);
    final codeError = needCode ? validateJamCode(_code.text) : null;
    setState(() {
      _nameError = nameError;
      _codeError = codeError;
    });
    final problem = nameError ?? codeError;
    if (problem != null) {
      ErrorPopup.show(InputError(problem), title: needCode ? "Can't join yet" : "Can't start the Jam yet");
      return false;
    }
    return true;
  }

  Future<void> _start() async {
    if (!_valid(needCode: false)) return;
    await _do((jam, name) => jam.create(name), "Couldn't start the Jam");
  }

  Future<void> _join() async {
    if (!_valid(needCode: true)) return;
    await _do((jam, name) => jam.join(normalizeJamCode(_code.text), name), "Couldn't join the Jam");
  }

  Future<void> _do(Future<void> Function(JamController jam, String name) action, String failTitle) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action(context.read<JamController>(), cleanJamName(_name.text));
    } catch (e) {
      if (mounted) setState(() => _error = friendlyError(e));
      ErrorPopup.show(e, title: failTitle);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(Gaps.xl, Gaps.xl, Gaps.xl, Gaps.xl),
      children: [
        Text('Swar Jam', style: Theme.of(context).textTheme.headlineMedium),
        const SizedBox(height: 6),
        const Text(
          'Listen together in sync. Start a Jam and share the code, or join a friend\'s.',
          style: TextStyle(color: SonivoColors.textSecondary),
        ),
        const SizedBox(height: 20),
        TextField(
          controller: _name,
          maxLength: 30,
          textCapitalization: TextCapitalization.words,
          onChanged: (_) {
            if (_nameError != null) setState(() => _nameError = validateJamName(_name.text));
          },
          decoration: InputDecoration(labelText: 'Your name in the Jam', errorText: _nameError),
        ),
        const SizedBox(height: 8),
        FilledButton.icon(
          onPressed: _busy ? null : _start,
          icon: const Icon(Icons.group_add),
          label: const Text('Start a Jam'),
        ),
        const SizedBox(height: 28),
        TextField(
          controller: _code,
          textCapitalization: TextCapitalization.characters,
          maxLength: 7,
          inputFormatters: [
            FilteringTextInputFormatter.allow(RegExp(r'[A-Za-z0-9 -]')),
            TextInputFormatter.withFunction((_, v) => v.copyWith(text: v.text.toUpperCase())),
          ],
          onChanged: (_) {
            if (_codeError != null) setState(() => _codeError = validateJamCode(_code.text));
          },
          onSubmitted: (_) => _busy ? null : _join(),
          decoration: InputDecoration(labelText: 'Jam code', hintText: 'e.g. K7MQ2P', errorText: _codeError),
        ),
        OutlinedButton.icon(
          onPressed: _busy ? null : _join,
          icon: const Icon(Icons.login),
          label: const Text('Join'),
        ),
        if (_busy) const Padding(padding: EdgeInsets.only(top: 16), child: LinearProgressIndicator()),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: 16),
            child: Text(_error!, style: const TextStyle(color: SonivoColors.danger)),
          ),
      ],
    );
  }
}

class _JamRoom extends StatelessWidget {
  const _JamRoom();

  Future<void> _guard(BuildContext context, Future<void> Function() f) async {
    try {
      await f();
    } catch (e) {
      ErrorPopup.show(e, title: 'Jam action failed');
    }
  }

  @override
  Widget build(BuildContext context) {
    final jam = context.watch<JamController>();

    if (jam.closedReason != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.music_off, size: 48, color: SonivoColors.textSecondary),
            const SizedBox(height: 12),
            Text(jam.closedReason!, style: const TextStyle(fontSize: 16)),
            const SizedBox(height: 12),
            FilledButton(onPressed: jam.dismissClosed, child: const Text('OK')),
          ],
        ),
      );
    }

    final status = switch (jam.connection) {
      JamConnection.connected => ('Connected', SonivoColors.accent),
      JamConnection.connecting => ('Connecting…', Colors.amber),
      JamConnection.reconnecting => ('Reconnecting…', Colors.amber),
      JamConnection.offline => ('Offline', SonivoColors.danger),
    };

    return ListView(
      padding: const EdgeInsets.symmetric(vertical: 12),
      children: [
        ListTile(
          title: Text(jam.name, style: const TextStyle(fontSize: 20, fontWeight: FontWeight.bold)),
          subtitle: Row(
            children: [
              Icon(Icons.circle, size: 10, color: status.$2),
              const SizedBox(width: 6),
              Text(status.$1),
              if (jam.connection == JamConnection.offline) ...[
                const SizedBox(width: 8),
                TextButton(onPressed: jam.reconnect, child: const Text('Retry')),
              ],
            ],
          ),
          trailing: jam.isAdmin
              ? TextButton(
                  style: TextButton.styleFrom(foregroundColor: SonivoColors.danger),
                  onPressed: () => _guard(context, jam.end),
                  child: const Text('End Jam'),
                )
              : TextButton(onPressed: jam.leave, child: const Text('Leave')),
        ),
        Card(
          margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          child: ListTile(
            leading: const Icon(Icons.key),
            title: Text(jam.code, style: const TextStyle(fontSize: 22, letterSpacing: 4, fontWeight: FontWeight.bold)),
            subtitle: const Text('Share this code to invite friends'),
            trailing: IconButton(
              tooltip: 'Copy code',
              icon: const Icon(Icons.copy),
              onPressed: () {
                Clipboard.setData(ClipboardData(text: jam.code));
                ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Jam code copied')));
              },
            ),
          ),
        ),
        if (jam.lastError != null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            child: Text(jam.lastError!, style: const TextStyle(color: SonivoColors.danger, fontSize: 13)),
          ),
        _NowPlayingCard(jam: jam),
        const _SectionTitle('Up next'),
        if (jam.queue.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text(
              jam.isAdmin || jam.allowParticipantAdd
                  ? 'The queue is empty. Add songs with "Add to Jam" in any song\'s menu.'
                  : 'The queue is empty. Only the admin can add songs in this Jam.',
              style: const TextStyle(color: SonivoColors.textSecondary),
            ),
          ),
        for (var i = 0; i < jam.queue.length; i++)
          TrackTile(
            track: jam.queue[i].track,
            onTap: () {
              if (jam.isAdmin) jam.jump(jam.queue[i].entryId);
            },
            trailing: i == jam.currentIndex
                ? const Icon(Icons.graphic_eq, color: SonivoColors.accent)
                : (jam.isAdmin || jam.queue[i].addedBy == jam.you)
                    ? IconButton(
                        tooltip: 'Remove from queue',
                        icon: const Icon(Icons.remove_circle_outline),
                        onPressed: () => jam.removeEntry(jam.queue[i].entryId),
                      )
                    : null,
          ),
        _SectionTitle('People (${jam.participants.length})'),
        for (final p in jam.participants)
          ListTile(
            leading: CircleAvatar(child: Text(p.name.isEmpty ? '?' : p.name[0].toUpperCase())),
            title: Text(p.id == jam.you ? '${p.name} (you)' : p.name),
            subtitle: Text(p.connected ? 'Listening' : 'Away'),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (p.isAdmin) const Chip(label: Text('Admin'), visualDensity: VisualDensity.compact),
                if (jam.isAdmin && !p.isAdmin)
                  IconButton(
                    tooltip: 'Remove from Jam',
                    icon: const Icon(Icons.person_remove_outlined),
                    onPressed: () async {
                      final ok = await showDialog<bool>(
                        context: context,
                        builder: (ctx) => AlertDialog(
                          title: Text('Remove ${p.name}?'),
                          content: const Text('They will be disconnected and cannot rejoin unless you allow it.'),
                          actions: [
                            TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
                            TextButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Remove')),
                          ],
                        ),
                      );
                      if (ok == true && context.mounted) {
                        await _guard(context, () => jam.removeParticipant(p.id));
                      }
                    },
                  ),
              ],
            ),
          ),
        if (jam.isAdmin && jam.removed.isNotEmpty) ...[
          const _SectionTitle('Removed'),
          for (final p in jam.removed)
            ListTile(
              leading: const Icon(Icons.block),
              title: Text(p.name),
              trailing: TextButton(onPressed: () => jam.allowRejoin(p.id), child: const Text('Allow rejoin')),
            ),
        ],
      ],
    );
  }
}

class _NowPlayingCard extends StatelessWidget {
  final JamController jam;
  const _NowPlayingCard({required this.jam});

  @override
  Widget build(BuildContext context) {
    final t = jam.nowPlaying;
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          children: [
            Text(t?.title ?? 'Nothing playing', style: const TextStyle(fontSize: 17, fontWeight: FontWeight.bold)),
            if (t != null) Text(t.artist, style: const TextStyle(color: SonivoColors.textSecondary)),
            if (jam.isAdmin)
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  IconButton(icon: const Icon(Icons.skip_previous), onPressed: jam.previous),
                  IconButton(
                    iconSize: 44,
                    icon: Icon(jam.playing ? Icons.pause_circle_filled : Icons.play_circle_filled),
                    onPressed: jam.queue.isEmpty ? null : jam.playPause,
                  ),
                  IconButton(icon: const Icon(Icons.skip_next), onPressed: jam.next),
                ],
              )
            else
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  jam.playing ? 'Playing in sync with the admin' : 'Paused by the admin',
                  style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  final String text;
  const _SectionTitle(this.text);

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 4),
        child: Text(text, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
      );
}
