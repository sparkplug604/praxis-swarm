"""Independent bounded-duration oracle frozen before the GHL-03 model run."""
import random
import unittest
from duration_parser import parse_duration
from duration_formatter import format_duration

class IndependentAcceptance(unittest.TestCase):
    def test_integer_oracle_and_roundtrips(self):
        rng = random.Random(29032026)
        values = [0, 1, 999, 1000, 59999, 60000, 3599999, 3600000, 999999999999, 1000000000000]
        values += [rng.randrange(1000000000001) for _ in range(1000)]
        for value in values:
            seconds, millis = divmod(value, 1000)
            minutes, seconds = divmod(seconds, 60)
            hours, minutes = divmod(minutes, 60)
            expected = f'{hours}:{minutes:02d}:{seconds:02d}.{millis:03d}'
            self.assertEqual(format_duration(value), expected)
            self.assertEqual(parse_duration(expected), value)
    def test_no_partial_matches_and_ascii_only(self):
        for text in ['0:00:00.000\r', '0:00:00.000\x00', '0:00:00.000\n\n', '٠:00:00.000', '0:00:00.000x', '0:00:00.00０']:
            with self.assertRaises(ValueError) as caught:
                parse_duration(text)
            self.assertEqual(str(caught.exception), 'invalid duration')
    def test_long_inputs_and_type_priority(self):
        for text in ['1'*100000 + ':00:00.000', '0:00:00.'+'0'*100000]:
            with self.assertRaises(ValueError) as caught:
                parse_duration(text)
            self.assertEqual(str(caught.exception),'invalid duration')
        for value in [True, False, 1000000000001.0, float('inf')]:
            with self.assertRaises(TypeError) as caught:
                format_duration(value)
            self.assertEqual(str(caught.exception),'milliseconds must be an integer')
        for value in [10**10000, -(10**10000)]:
            with self.assertRaises(ValueError) as caught:
                format_duration(value)
            self.assertEqual(str(caught.exception),'milliseconds out of range')

if __name__ == '__main__':
    unittest.main()
