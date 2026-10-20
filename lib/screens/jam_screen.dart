import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/jam_validation.dart';
import '../core/theme.dart';
import '../services/jam_service.dart';
import '../widgets/jam_picker.dart';
import '../widgets/ui.dart';

/// Swar Jam: listen together. Everyone plays the same queue on their own
/// device, in sync; only the admin controls playback.
class JamScreen extends StatelessWidget {
  const JamScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final jam = context.watch<JamController>();
    return SafeArea(
      bottom: false,
      child: PageWidth(child: jam.jamId == null ? const _JamLobby() : const _JamRoom()),
    );
  }
}

/// Content wider than this puts panels side by side.
const _twoColumns = 760.0;

/// The page title row, aligned with the other tabs' titles.
class _Header extends StatelessWidget {
  final String title;
  final Widget? subtitle;
  const _Header({required this.title, this.subtitle});

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title,
                  style: Theme.of(context).textTheme.headlineMedium, maxLines: 1, overflow: TextOverflow.ellipsis),
              if (subtitle != null) ...[const SizedBox(height: Gaps.xs), subtitle!],
            ],
          ),
        ),
      ],
    );
  }
}

/// A rounded surface that groups one part of the Jam.
class _Panel extends StatelessWidget {
  final String? title;
  final Widget? action;
  final Widget child;
  final EdgeInsetsGeometry padding;
  const _Panel({this.title, this.action, required this.child, this.padding = const EdgeInsets.all(Gaps.lg)});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: SonivoColors.surface,
        borderRadius: BorderRadius.circular(Gaps.radiusLg),
        border: Border.all(color: SonivoColors.outline),
      ),
      padding: padding,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          if (title != null) ...[
            SizedBox(
              height: 40,
              child: Row(
                children: [
                  Expanded(child: Text(title!, style: Theme.of(context).textTheme.titleMedium)),
                  if (action != null) action!,
                ],
              ),
            ),
            const SizedBox(height: Gaps.sm),
          ],
          child,
        ],
      ),
    );
  }
}

/// A square icon badge with the brand gradient.
class _Badge extends StatelessWidget {
  final IconData icon;
  final double size;
  const _Badge(this.icon, {this.size = 48});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(gradient: SonivoColors.brandGradient, borderRadius: BorderRadius.circular(size * 0.3)),
      child: Icon(icon, color: Colors.white, size: size * 0.5),
    );
  }
}

/// Lays out [children] as equal columns on wide screens and a stack on
/// phones, with the same gap both ways.
class _Columns extends StatelessWidget {
  final List<Widget> children;
  final List<int>? flex;
  const _Columns({required this.children, this.flex});

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(builder: (context, c) {
      if (c.maxWidth < _twoColumns) {
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (var i = 0; i < children.length; i++) ...[
              if (i > 0) const SizedBox(height: Gaps.lg),
              children[i],
            ],
          ],
        );
      }
      return IntrinsicHeight(
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (var i = 0; i < children.length; i++) ...[
              if (i > 0) const SizedBox(width: Gaps.lg),
              Expanded(flex: flex?[i] ?? 1, child: children[i]),
            ],
          ],
        ),
      );
    });
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

  /// One of the two choices: same shape, so the pair lines up.
  Widget _choice({
    required IconData icon,
    required String title,
    required String body,
    required Widget field,
    required Widget button,
  }) {
    return _Panel(
      padding: const EdgeInsets.all(Gaps.xl),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              _Badge(icon, size: 44),
              const SizedBox(width: Gaps.md),
              Expanded(child: Text(title, style: Theme.of(context).textTheme.titleLarge)),
            ],
          ),
          const SizedBox(height: Gaps.md),
          Text(body, style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: Gaps.lg),
          field,
          const SizedBox(height: Gaps.md),
          SizedBox(height: 48, child: button),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final side = Gaps.pagePadding(context);
    final theme = Theme.of(context);

    return ListView(
      padding: EdgeInsets.fromLTRB(side, Gaps.xl, side, Gaps.xxl),
      children: [
        _Header(
          title: 'Jam',
          subtitle: Text('Listen together in sync, each on your own device.',
              style: theme.textTheme.bodyMedium?.copyWith(color: SonivoColors.textSecondary)),
        ),
        const SizedBox(height: Gaps.xl),
        const _Steps(),
        const SizedBox(height: Gaps.xl),
        Text('Your name', style: theme.textTheme.titleSmall),
        const SizedBox(height: Gaps.sm),
        TextField(
          controller: _name,
          maxLength: 30,
          textCapitalization: TextCapitalization.words,
          onChanged: (_) {
            if (_nameError != null) setState(() => _nameError = validateJamName(_name.text));
          },
          decoration: InputDecoration(
            hintText: 'How friends will see you',
            prefixIcon: const Icon(PhosphorIconsRegular.user),
            errorText: _nameError,
            counterText: '',
          ),
        ),
        const SizedBox(height: Gaps.lg),
        _Columns(children: [
          _choice(
            icon: PhosphorIconsRegular.broadcast,
            title: 'Start a Jam',
            body: 'You control playback. Friends who join can add songs to the shared queue.',
            field: const _InfoField(icon: PhosphorIconsRegular.key, text: 'A code is made for you to share'),
            button: FilledButton.icon(
              onPressed: _busy ? null : _start,
              icon: const Icon(PhosphorIconsRegular.play),
              label: const Text('Start a Jam'),
            ),
          ),
          _choice(
            icon: PhosphorIconsRegular.signIn,
            title: 'Join a Jam',
            body: 'Enter the code a friend shared to hear what they are playing.',
            field: TextField(
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
              style: const TextStyle(letterSpacing: 2, fontWeight: FontWeight.w600),
              decoration: InputDecoration(
                hintText: 'Jam code, e.g. K7MQ2P',
                hintStyle: const TextStyle(letterSpacing: 0, color: SonivoColors.textMuted, fontSize: 14),
                prefixIcon: const Icon(PhosphorIconsRegular.hash),
                errorText: _codeError,
                counterText: '',
              ),
            ),
            button: OutlinedButton.icon(
              onPressed: _busy ? null : _join,
              icon: const Icon(PhosphorIconsRegular.signIn),
              label: const Text('Join'),
            ),
          ),
        ]),
        if (_busy) const Padding(padding: EdgeInsets.only(top: Gaps.lg), child: LinearProgressIndicator()),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: Gaps.lg),
            child: Text(_error!, style: const TextStyle(color: SonivoColors.danger)),
          ),
      ],
    );
  }
}

/// A read-only line the same height as a text field, so both choices match.
class _InfoField extends StatelessWidget {
  final IconData icon;
  final String text;
  const _InfoField({required this.icon, required this.text});

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 51,
      padding: const EdgeInsets.symmetric(horizontal: 14),
      decoration: BoxDecoration(
        color: SonivoColors.surfaceHigh.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(Gaps.radius),
      ),
      child: Row(
        children: [
          Icon(icon, size: 20, color: SonivoColors.textSecondary),
          const SizedBox(width: 18),
          Expanded(
            child:
                Text(text, style: Theme.of(context).textTheme.bodySmall, maxLines: 1, overflow: TextOverflow.ellipsis),
          ),
        ],
      ),
    );
  }
}

/// How a Jam works, in three equal steps.
class _Steps extends StatelessWidget {
  const _Steps();

  @override
  Widget build(BuildContext context) {
    const steps = [
      (PhosphorIconsRegular.usersThree, 'Start or join', 'One person starts, everyone else joins.'),
      (PhosphorIconsRegular.shareNetwork, 'Share the code', 'Send the six character code to friends.'),
      (PhosphorIconsRegular.waveform, 'Listen in sync', 'The same song, at the same moment.'),
    ];
    return LayoutBuilder(builder: (context, c) {
      final narrow = c.maxWidth < _twoColumns;
      Widget step(int i) {
        final (icon, title, body) = steps[i];
        final text = Column(
          crossAxisAlignment: narrow ? CrossAxisAlignment.start : CrossAxisAlignment.center,
          children: [
            Text(title, style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 2),
            Text(body,
                style: Theme.of(context).textTheme.bodySmall, textAlign: narrow ? TextAlign.start : TextAlign.center),
          ],
        );
        final badge = Stack(
          clipBehavior: Clip.none,
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(color: SonivoColors.surfaceHigh, borderRadius: BorderRadius.circular(14)),
              child: Icon(icon, color: SonivoColors.accent, size: 22),
            ),
            Positioned(
              right: -6,
              top: -6,
              child: Container(
                width: 20,
                height: 20,
                alignment: Alignment.center,
                decoration: const BoxDecoration(shape: BoxShape.circle, gradient: SonivoColors.brandGradient),
                child: Text('${i + 1}',
                    style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w700, color: Colors.white)),
              ),
            ),
          ],
        );
        if (narrow) {
          return Row(children: [badge, const SizedBox(width: Gaps.lg), Expanded(child: text)]);
        }
        return Column(children: [badge, const SizedBox(height: Gaps.md), text]);
      }

      return _Panel(
        padding: const EdgeInsets.symmetric(horizontal: Gaps.xl, vertical: Gaps.xl),
        child: narrow
            ? Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  for (var i = 0; i < steps.length; i++) ...[if (i > 0) const SizedBox(height: Gaps.lg), step(i)],
                ],
              )
            : Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [for (var i = 0; i < steps.length; i++) Expanded(child: step(i))],
              ),
      );
    });
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

  Future<void> _confirmRemove(BuildContext context, JamController jam, JamParticipant p) async {
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
  }

  @override
  Widget build(BuildContext context) {
    final jam = context.watch<JamController>();

    if (jam.closedReason != null) {
      return EmptyState(
        icon: PhosphorIconsRegular.musicNotesMinus,
        title: jam.closedReason!,
        message: 'Start a new Jam or join another one any time.',
        action: FilledButton(onPressed: jam.dismissClosed, child: const Text('OK')),
      );
    }

    final side = Gaps.pagePadding(context);
    final canAdd = jam.isAdmin || jam.allowParticipantAdd;

    final queue = _Panel(
      title: 'Up next',
      action: canAdd
          ? FilledButton.tonalIcon(
              style: FilledButton.styleFrom(
                minimumSize: const Size(0, 36),
                padding: const EdgeInsets.symmetric(horizontal: Gaps.md),
                visualDensity: VisualDensity.compact,
              ),
              onPressed: () => showJamSongPicker(context),
              icon: const Icon(PhosphorIconsRegular.listPlus, size: 18),
              label: const Text('Add songs'),
            )
          : null,
      padding: const EdgeInsets.fromLTRB(Gaps.lg, Gaps.lg, Gaps.lg, Gaps.sm),
      child: jam.queue.isEmpty
          ? Padding(
              padding: const EdgeInsets.symmetric(vertical: Gaps.xl),
              child: Column(
                children: [
                  const Icon(PhosphorIconsRegular.queue, size: 32, color: SonivoColors.textMuted),
                  const SizedBox(height: Gaps.sm),
                  Text(
                    canAdd
                        ? 'The queue is empty. Tap Add songs, or use Add to Jam in any song\'s menu.'
                        : 'The queue is empty. Only the admin can add songs in this Jam.',
                    style: Theme.of(context).textTheme.bodySmall,
                    textAlign: TextAlign.center,
                  ),
                ],
              ),
            )
          : Column(
              children: [
                for (var i = 0; i < jam.queue.length; i++)
                  _QueueRow(
                    entry: jam.queue[i],
                    current: i == jam.currentIndex,
                    addedBy: jam.participantName(jam.queue[i].addedBy),
                    onTap: jam.isAdmin ? () => jam.jump(jam.queue[i].entryId) : null,
                    onRemove: (jam.isAdmin || jam.queue[i].addedBy == jam.you) && i != jam.currentIndex
                        ? () => jam.removeEntry(jam.queue[i].entryId)
                        : null,
                  ),
              ],
            ),
    );

    final people = _Panel(
      title: 'People (${jam.participants.length})',
      padding: const EdgeInsets.fromLTRB(Gaps.lg, Gaps.lg, Gaps.lg, Gaps.sm),
      child: Column(
        children: [
          for (final p in jam.participants)
            _PersonRow(
              person: p,
              isYou: p.id == jam.you,
              onRemove: jam.isAdmin && !p.isAdmin ? () => _confirmRemove(context, jam, p) : null,
            ),
          if (jam.isAdmin && jam.removed.isNotEmpty) ...[
            const Divider(height: Gaps.xl),
            for (final p in jam.removed)
              SizedBox(
                height: 52,
                child: Row(
                  children: [
                    const SizedBox(
                      width: 36,
                      child: Icon(PhosphorIconsRegular.prohibit, color: SonivoColors.textMuted, size: 20),
                    ),
                    const SizedBox(width: Gaps.md),
                    Expanded(child: Text(p.name, maxLines: 1, overflow: TextOverflow.ellipsis)),
                    TextButton(onPressed: () => jam.allowRejoin(p.id), child: const Text('Allow rejoin')),
                  ],
                ),
              ),
          ],
        ],
      ),
    );

    return ListView(
      padding: EdgeInsets.fromLTRB(side, Gaps.xl, side, Gaps.xxl),
      children: [
        _JamBanner(jam: jam, onEnd: () => _guard(context, jam.end)),
        if (jam.lastError != null)
          Padding(
            padding: const EdgeInsets.only(top: Gaps.md),
            child: Text(jam.lastError!, style: const TextStyle(color: SonivoColors.danger, fontSize: 13)),
          ),
        const SizedBox(height: Gaps.lg),
        _Columns(flex: const [3, 2], children: [_NowPlayingPanel(jam: jam), _InvitePanel(jam: jam)]),
        const SizedBox(height: Gaps.lg),
        _Columns(flex: const [3, 2], children: [queue, people]),
      ],
    );
  }
}

/// The top of a Jam: who is listening, how the connection is doing, and
/// the one action that ends or leaves it.
class _JamBanner extends StatelessWidget {
  final JamController jam;
  final VoidCallback onEnd;
  const _JamBanner({required this.jam, required this.onEnd});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    // Only say something about the connection when it is not working.
    final problem = switch (jam.connection) {
      JamConnection.connected => null,
      JamConnection.connecting => 'Connecting…',
      JamConnection.reconnecting => 'Reconnecting…',
      JamConnection.offline => 'Offline',
    };
    final listening = jam.participants.where((p) => p.connected).length;

    final details = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(jam.name, style: theme.textTheme.headlineSmall, maxLines: 1, overflow: TextOverflow.ellipsis),
        const SizedBox(height: Gaps.xs),
        Row(
          children: [
            Flexible(
              child: Text(
                problem ??
                    '${jam.isAdmin ? 'You are the host' : 'The host controls playback'}  ·  '
                        '$listening ${listening == 1 ? 'person' : 'people'} listening',
                style: problem == null
                    ? theme.textTheme.bodySmall
                    : theme.textTheme.bodySmall?.copyWith(color: SonivoColors.danger),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            ),
            if (jam.connection == JamConnection.offline) _retry(),
          ],
        ),
      ],
    );

    final action = jam.isAdmin
        ? FilledButton.icon(
            style: FilledButton.styleFrom(
              backgroundColor: SonivoColors.danger.withValues(alpha: 0.14),
              foregroundColor: SonivoColors.danger,
              minimumSize: const Size(0, 44),
              padding: const EdgeInsets.symmetric(horizontal: Gaps.lg),
            ),
            onPressed: onEnd,
            icon: const Icon(PhosphorIconsFill.stop, size: 16),
            label: const Text('End Jam'),
          )
        : FilledButton.icon(
            style: FilledButton.styleFrom(
              backgroundColor: SonivoColors.surfaceHigher,
              foregroundColor: SonivoColors.textPrimary,
              minimumSize: const Size(0, 44),
              padding: const EdgeInsets.symmetric(horizontal: Gaps.lg),
            ),
            onPressed: jam.leave,
            icon: const Icon(PhosphorIconsRegular.signOut, size: 18),
            label: const Text('Leave Jam'),
          );

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: Gaps.lg, vertical: Gaps.xl),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(Gaps.radiusLg),
        border: Border.all(color: SonivoColors.outline),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            SonivoColors.accent.withValues(alpha: 0.16),
            SonivoColors.violet.withValues(alpha: 0.10),
            SonivoColors.surface,
          ],
          stops: const [0, 0.45, 1],
        ),
      ),
      child: LayoutBuilder(builder: (context, c) {
        final narrow = c.maxWidth < 480;
        final top = Row(
          children: [
            const _Badge(PhosphorIconsRegular.broadcast, size: 56),
            const SizedBox(width: Gaps.lg),
            Expanded(child: details),
            if (!narrow) ...[const SizedBox(width: Gaps.lg), action],
          ],
        );
        if (!narrow) return top;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [top, const SizedBox(height: Gaps.lg), action],
        );
      }),
    );
  }

  Widget _retry() => Padding(
        padding: const EdgeInsets.only(left: Gaps.sm),
        child: TextButton(onPressed: jam.reconnect, child: const Text('Retry')),
      );
}

class _NowPlayingPanel extends StatelessWidget {
  final JamController jam;
  const _NowPlayingPanel({required this.jam});

  @override
  Widget build(BuildContext context) {
    final t = jam.nowPlaying;
    final theme = Theme.of(context);
    return _Panel(
      title: 'Now playing',
      child: Row(
        children: [
          Artwork(url: t?.thumbnailUrl ?? '', size: 96, radius: Gaps.radius),
          const SizedBox(width: Gaps.lg),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(t?.title ?? 'Nothing playing yet',
                    style: theme.textTheme.titleMedium, maxLines: 1, overflow: TextOverflow.ellipsis),
                const SizedBox(height: 2),
                Text(
                  t?.artist ?? (jam.isAdmin ? 'Add songs to get started' : 'Waiting for the host'),
                  style: theme.textTheme.bodySmall,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: Gaps.md),
                if (jam.isAdmin)
                  // Pulled left so the first button's glyph lines up with the title.
                  Transform.translate(
                    offset: const Offset(-8, 0),
                    child: Row(
                      children: [
                        IconButton(
                          tooltip: 'Previous',
                          icon: const Icon(PhosphorIconsFill.skipBack),
                          onPressed: jam.queue.isEmpty ? null : jam.previous,
                        ),
                        const SizedBox(width: Gaps.xs),
                        GradientPlayButton(
                          size: 44,
                          playing: jam.playing,
                          onPressed: jam.queue.isEmpty ? null : jam.playPause,
                        ),
                        const SizedBox(width: Gaps.xs),
                        IconButton(
                          tooltip: 'Next',
                          icon: const Icon(PhosphorIconsFill.skipForward),
                          onPressed: jam.queue.isEmpty ? null : jam.next,
                        ),
                      ],
                    ),
                  )
                else
                  Pill(jam.playing ? 'Playing in sync with the host' : 'Paused by the host',
                      icon: jam.playing ? PhosphorIconsRegular.waveform : PhosphorIconsRegular.pause),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _InvitePanel extends StatelessWidget {
  final JamController jam;
  const _InvitePanel({required this.jam});

  @override
  Widget build(BuildContext context) {
    void copy() {
      Clipboard.setData(ClipboardData(text: jam.code));
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Jam code copied')));
    }

    return _Panel(
      title: 'Invite friends',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            height: 64,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: SonivoColors.surfaceHigh,
              borderRadius: BorderRadius.circular(Gaps.radius),
              border: Border.all(color: SonivoColors.accent.withValues(alpha: 0.35)),
            ),
            child: SelectableText(
              jam.code,
              style: const TextStyle(fontSize: 28, letterSpacing: 8, fontWeight: FontWeight.w800),
            ),
          ),
          const SizedBox(height: Gaps.md),
          Row(
            children: [
              Expanded(
                child: Text('Friends enter this code in their Jam tab.', style: Theme.of(context).textTheme.bodySmall),
              ),
              TextButton.icon(
                onPressed: copy,
                icon: const Icon(PhosphorIconsRegular.copy, size: 18),
                label: const Text('Copy'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _QueueRow extends StatelessWidget {
  final JamEntry entry;
  final bool current;
  final String addedBy;
  final VoidCallback? onTap;
  final VoidCallback? onRemove;
  const _QueueRow({required this.entry, required this.current, required this.addedBy, this.onTap, this.onRemove});

  @override
  Widget build(BuildContext context) {
    final t = entry.track;
    final theme = Theme.of(context);
    return Material(
      color: current ? SonivoColors.accent.withValues(alpha: 0.08) : Colors.transparent,
      borderRadius: BorderRadius.circular(Gaps.radiusSm),
      child: InkWell(
        borderRadius: BorderRadius.circular(Gaps.radiusSm),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: Gaps.sm, vertical: Gaps.sm),
          child: Row(
            children: [
              Artwork(url: t.thumbnailUrl, size: 44),
              const SizedBox(width: Gaps.md),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      t.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall?.copyWith(color: current ? SonivoColors.accent : null),
                    ),
                    Text(
                      addedBy.isEmpty ? t.artist : '${t.artist}  ·  added by $addedBy',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall,
                    ),
                  ],
                ),
              ),
              SizedBox(
                width: 40,
                child: current
                    ? const Icon(PhosphorIconsRegular.waveform, color: SonivoColors.accent, size: 20)
                    : onRemove == null
                        ? null
                        : IconButton(
                            tooltip: 'Remove from queue',
                            icon: const Icon(PhosphorIconsRegular.minusCircle, size: 20),
                            onPressed: onRemove,
                          ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _PersonRow extends StatelessWidget {
  final JamParticipant person;
  final bool isYou;
  final VoidCallback? onRemove;
  const _PersonRow({required this.person, required this.isYou, this.onRemove});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final initial = person.name.isEmpty ? '?' : person.name[0].toUpperCase();
    return SizedBox(
      height: 56,
      child: Row(
        children: [
          Stack(
            children: [
              Container(
                width: 36,
                height: 36,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  gradient: person.isAdmin ? SonivoColors.brandGradient : null,
                  color: person.isAdmin ? null : SonivoColors.surfaceHigher,
                ),
                child: Text(initial, style: const TextStyle(fontWeight: FontWeight.w700, color: Colors.white)),
              ),
              Positioned(
                right: 0,
                bottom: 0,
                child: Container(
                  width: 11,
                  height: 11,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    color: person.connected ? const Color(0xFF3DDC84) : SonivoColors.textMuted,
                    border: Border.all(color: SonivoColors.surface, width: 2),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(width: Gaps.md),
          Expanded(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(isYou ? '${person.name} (you)' : person.name,
                    style: theme.textTheme.titleSmall, maxLines: 1, overflow: TextOverflow.ellipsis),
                Text(person.connected ? 'Listening' : 'Away', style: theme.textTheme.bodySmall),
              ],
            ),
          ),
          if (person.isAdmin) const Pill('Host'),
          if (onRemove != null)
            IconButton(
              tooltip: 'Remove from Jam',
              icon: const Icon(PhosphorIconsRegular.userMinus, size: 20),
              onPressed: onRemove,
            ),
        ],
      ),
    );
  }
}
