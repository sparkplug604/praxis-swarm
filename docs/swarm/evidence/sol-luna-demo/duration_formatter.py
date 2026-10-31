"""Bounded canonical duration formatter; see CONTRACT.md."""


def format_duration(milliseconds):
    if type(milliseconds) is not int:
        raise TypeError("milliseconds must be an integer")
    if milliseconds < 0 or milliseconds > 1_000_000_000_000:
        raise ValueError("milliseconds out of range")

    total_seconds, milliseconds_part = divmod(milliseconds, 1000)
    hours, remainder = divmod(total_seconds, 3600)
    minutes, seconds = divmod(remainder, 60)
    return f"{hours}:{minutes:02d}:{seconds:02d}.{milliseconds_part:03d}"
