"""Bounded canonical duration parser; see CONTRACT.md."""


def parse_duration(text):
    if not isinstance(text, str):
        raise TypeError("duration must be a string")

    if not 11 <= len(text) <= 16:
        raise ValueError("invalid duration")

    colon = text.find(":")
    if colon < 1 or colon > 6 or text.find(":", colon + 1) != colon + 3:
        raise ValueError("invalid duration")
    if colon > 1 and text[0] == "0":
        raise ValueError("invalid duration")
    if text[colon + 3] != ":" or text[colon + 6] != ".":
        raise ValueError("invalid duration")
    if len(text) != colon + 10:
        raise ValueError("invalid duration")

    hour_text = text[:colon]
    minute_text = text[colon + 1:colon + 3]
    second_text = text[colon + 4:colon + 6]
    millisecond_text = text[colon + 7:]
    if not (hour_text.isascii() and hour_text.isdecimal() and
            minute_text.isascii() and minute_text.isdecimal() and
            second_text.isascii() and second_text.isdecimal() and
            millisecond_text.isascii() and millisecond_text.isdecimal()):
        raise ValueError("invalid duration")

    minutes = int(minute_text)
    seconds = int(second_text)
    if minutes > 59 or seconds > 59:
        raise ValueError("invalid duration")

    total = (int(hour_text) * 3600000 + minutes * 60000 +
             seconds * 1000 + int(millisecond_text))
    if total > 1000000000000:
        raise ValueError("invalid duration")
    return total
