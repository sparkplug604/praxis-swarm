import unittest
from duration_formatter import format_duration


class DurationFormatterTests(unittest.TestCase):
    def test_exact_values(self):
        cases = {0: "0:00:00.000", 1: "0:00:00.001", 999: "0:00:00.999",
                 1000: "0:00:01.000", 59999: "0:00:59.999", 60000: "0:01:00.000",
                 3661001: "1:01:01.001", 86400000: "24:00:00.000",
                 1000000000000: "277777:46:40.000"}
        for value, expected in cases.items():
            with self.subTest(value=value):
                actual = format_duration(value)
                self.assertIs(type(actual), str)
                self.assertEqual(actual, expected)

    def test_unit_boundaries(self):
        self.assertEqual(format_duration(3599999), "0:59:59.999")
        self.assertEqual(format_duration(3600000), "1:00:00.000")
        self.assertEqual(format_duration(3600001), "1:00:00.001")
        self.assertEqual(format_duration(999999999999), "277777:46:39.999")

    def test_rejects_non_integers(self):
        class DerivedInt(int):
            pass
        values = (None, True, False, 1.0, "1", [], {}, DerivedInt(1), float("nan"))
        for value in values:
            with self.subTest(value=type(value).__name__):
                with self.assertRaises(TypeError) as caught:
                    format_duration(value)
                self.assertEqual(str(caught.exception), "milliseconds must be an integer")
        self.assertEqual(values[5:7], ([], {}))

    def test_rejects_out_of_range(self):
        for value in (-1, -1000, 1000000000001):
            with self.subTest(value=value):
                with self.assertRaises(ValueError) as caught:
                    format_duration(value)
                self.assertEqual(str(caught.exception), "milliseconds out of range")

    def test_huge_integer_rejects_portably(self):
        for value in (10 ** 5001, -(10 ** 5001)):
            with self.assertRaises(ValueError) as caught:
                format_duration(value)
            self.assertEqual(str(caught.exception), "milliseconds out of range")


if __name__ == "__main__":
    unittest.main()
