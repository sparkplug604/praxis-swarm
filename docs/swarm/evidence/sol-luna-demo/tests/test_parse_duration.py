import unittest
from duration_parser import parse_duration


class DurationParserTests(unittest.TestCase):
    def test_exact_values(self):
        cases = {"0:00:00.000": 0, "0:00:00.001": 1,
                 "0:00:59.999": 59999, "0:01:00.000": 60000,
                 "1:01:01.001": 3661001, "24:00:00.000": 86400000,
                 "277777:46:40.000": 1000000000000}
        for text, expected in cases.items():
            with self.subTest(text=text):
                actual = parse_duration(text)
                self.assertIs(type(actual), int)
                self.assertEqual(actual, expected)

    def test_independent_units(self):
        for hours in (0, 1, 13, 999, 277776):
            for minutes, seconds, millis in ((0, 0, 0), (59, 59, 999), (7, 31, 4)):
                text = f"{hours}:{minutes:02}:{seconds:02}.{millis:03}"
                expected = hours * 3600000 + minutes * 60000 + seconds * 1000 + millis
                self.assertEqual(parse_duration(text), expected)

    def test_rejects_non_strings(self):
        values = (None, True, 0, 1.5, b"0:00:00.000", [], {})
        for value in values:
            with self.subTest(value=type(value).__name__):
                with self.assertRaises(TypeError) as caught:
                    parse_duration(value)
                self.assertEqual(str(caught.exception), "duration must be a string")
        self.assertEqual(values[-2:], ([], {}))

    def test_rejects_invalid_text(self):
        values = ("", "00:00:00.000", "01:00:00.000", "-1:00:00.000",
                  "+1:00:00.000", "1:0:00.000", "1:00:0.000", "1:60:00.000",
                  "1:00:60.000", "1:00:00.00", "1:00:00.0000", " 0:00:00.000",
                  "0:00:00.000 ", "0:00:00.000\n", "１:00:00.000", "0:٠٠:00.000",
                  "277777:46:40.001", "999999:59:59.999", "1000000:00:00.000")
        for value in values:
            with self.subTest(value=value):
                with self.assertRaises(ValueError) as caught:
                    parse_duration(value)
                self.assertEqual(str(caught.exception), "invalid duration")

    def test_huge_text_rejects_portably(self):
        with self.assertRaises(ValueError) as caught:
            parse_duration("9" * 5001 + ":00:00.000")
        self.assertEqual(str(caught.exception), "invalid duration")


if __name__ == "__main__":
    unittest.main()
