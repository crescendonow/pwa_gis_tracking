"""Pipe type and function keyword mappings."""

PIPE_TYPES = {
    "pvc": "PVC", "พีวีซี": "PVC",
    "ac": "AC", "ซีเมนต์ใยหิน": "AC", "เอซี": "AC",
    "hdpe": "HDPE", "เอชดีพีอี": "HDPE",
    "di": "DI", "เหล็กหล่อเหนียว": "DI",
    "ci": "CI", "เหล็กหล่อ": "CI",
    "gs": "GS", "เหล็กอาบสังกะสี": "GS",
    "st": "ST", "เหล็ก": "ST",
    "pb": "PB",
    "grp": "GRP",
    "pvc-o": "PVC_O", "pvc_o": "PVC_O", "พีวีซีโอ": "PVC_O",
}
_SORTED_PIPE_TYPE_KW = sorted(PIPE_TYPES.keys(), key=len, reverse=True)

PIPE_FUNCTIONS = {
    "ท่อส่งน้ำระหว่าง": ("4", "ท่อส่งน้ำระหว่างสถานี"),
    "ท่อส่งน้ำ": ("1", "ท่อส่งน้ำ"),
    "ท่อจ่ายน้ำ": ("2", "ท่อจ่ายน้ำ"),
    "ท่อน้ำดิบ": ("5", "ท่อน้ำดิบ"),
    "ท่อปลอก": ("6", "ท่อปลอก"),
}
_SORTED_PIPE_FUNC_KW = sorted(PIPE_FUNCTIONS.keys(), key=len, reverse=True)
