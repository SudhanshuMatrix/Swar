/// Checks for the Jam lobby, so mistakes are caught before calling the
/// server. They match the server's rules for names and codes.
library;

const jamCodeLength = 6;
const _codeAlphabet = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789'; // no 0/O, 1/I/L
final _nameChars = RegExp(r"^[\p{L}\p{M}\p{N} ._'\-]+$", unicode: true);
final _hasLetter = RegExp(r'\p{L}', unicode: true);

/// Collapses repeated spaces and trims.
String cleanJamName(String name) => name.trim().split(RegExp(r'\s+')).where((p) => p.isNotEmpty).join(' ');

/// Uppercases and removes spaces and dashes, as people often type
/// "k7m-q2p" or "K7M Q2P".
String normalizeJamCode(String code) => code.toUpperCase().replaceAll(RegExp(r'[\s-]'), '');

/// Returns what is wrong with [name], or null when it is fine.
String? validateJamName(String name) {
  final n = cleanJamName(name);
  if (n.isEmpty) return 'Enter your name so others know who joined.';
  if (n.runes.length < 2) return 'Your name needs at least 2 characters.';
  if (n.runes.length > 30) return 'Your name can be at most 30 characters.';
  if (!_nameChars.hasMatch(n)) return "Use only letters, numbers, spaces and . _ - ' in your name.";
  if (!_hasLetter.hasMatch(n)) return 'Your name needs at least one letter.';
  return null;
}

/// Returns what is wrong with [code], or null when it is fine.
String? validateJamCode(String code) {
  final c = normalizeJamCode(code);
  if (c.isEmpty) return 'Enter the Jam code your friend shared.';
  if (c.length != jamCodeLength) return 'A Jam code has $jamCodeLength characters, like K7MQ2P.';
  for (final ch in c.split('')) {
    if (!_codeAlphabet.contains(ch)) {
      if ('0O1IL'.contains(ch)) return 'Jam codes never use 0, O, 1, I or L. Check the code and try again.';
      return 'Jam codes use only letters and numbers.';
    }
  }
  return null;
}
