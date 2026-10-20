import 'package:flutter_test/flutter_test.dart';
import 'package:sonivo/core/jam_validation.dart';

void main() {
  group('name', () {
    test('accepts normal names', () {
      for (final n in ['Riya', 'Aman K.', "D'Souza", 'राहुल', 'dj_mix-2']) {
        expect(validateJamName(n), isNull, reason: n);
      }
    });
    test('rejects empty, short, long and odd names', () {
      expect(validateJamName('   '), contains('Enter your name'));
      expect(validateJamName('a'), contains('at least 2'));
      expect(validateJamName('x' * 31), contains('at most 30'));
      expect(validateJamName('<script>'), contains('Use only'));
      expect(validateJamName('1234'), contains('one letter'));
    });
    test('cleans spaces', () => expect(cleanJamName('  Aman   K  '), 'Aman K'));
  });

  group('code', () {
    test('accepts valid codes typed loosely', () {
      expect(validateJamCode('k7mq2p'), isNull);
      expect(validateJamCode('K7M-Q2P'), isNull);
      expect(normalizeJamCode('k7m q2p'), 'K7MQ2P');
    });
    test('rejects empty, wrong length and confusable characters', () {
      expect(validateJamCode(''), contains('Enter the Jam code'));
      expect(validateJamCode('K7MQ'), contains('6 characters'));
      expect(validateJamCode('K7MQ2O'), contains('never use 0, O, 1, I or L'));
    });
  });
}
