"""Bounded canonical duration parser; see CONTRACT.md."""


def parse_duration(text):
    if not isinstance(text, str):
        raise TypeError("duration must be a string")

    if not 11 <= len(text) <= 16:
        raise ValueError("invalid duration")

    hours_end = len(text) - 10
    hours_text = text[:hours_end]
    if (not hours_text or
            (len(hours_text) > 1 and hours_text[0] == "0") or
            any(char < "0" or char > "9" for char in hours_text)):
        raise ValueError("invalid duration")

    if (text[hours_end] != ":" or text[-7] != ":" or text[-4] != "." or
            any(char < "0" or char > "9"
                for char in text[hours_end + 1:hours_end + 3] +
                text[hours_end + 4:hours_end + 6] + text[-3:])):
        raise ValueError("invalid duration")

    minutes_text = text[hours_end + 1:hours_end + 3]
    seconds_text = text[hours_end + 4:hours_end + 6]
    millis_text = text[-3:]
    minutes = int(minutes_text)
    seconds = int(seconds_text)
    if minutes > 59 or seconds > 59:
        raise ValueError("invalid duration")

    total = (int(hours_text) * 3600000 + seconds * 1000 +
             minutes * 60000 + int(millis_text))
    if total > 1_000_000_000_000:
        raise ValueError("invalid duration")
    return total
