import unittest
from duration_parser import parse_duration


class TruncatedDurationTests(unittest.TestCase):
    def test_rejects_truncated_fields(self):
        for complete in ("0:00:00.000", "12:34:56.789", "123:00:00.000",
                         "1234:00:00.000", "12345:00:00.000", "123456:00:00.000"):
            for end in range(len(complete)):
                text = complete[:end]
                with self.subTest(text=text):
                    with self.assertRaises(ValueError) as caught:
                        parse_duration(text)
                    self.assertEqual(str(caught.exception), "invalid duration")
