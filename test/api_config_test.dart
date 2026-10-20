import 'package:flutter_test/flutter_test.dart';
import 'package:sonivo/core/constants.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('backend URL comes from the bundled .env', () async {
    await ApiConfig.load();
    expect(ApiConfig.baseUrl, startsWith('https://'));
    expect(ApiConfig.baseUrl.endsWith('/'), isFalse);
  });
}
