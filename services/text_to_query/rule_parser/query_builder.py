"""Helper to build MongoDB $match stages."""


def build_match(pwa_code, pipe_type=None, pipe_func_id=None, size=None,
                size_gte=None, size_lt=None, extra=None, layer=None):
    """Build a $match stage with optional filters."""
    match = {}
    if pwa_code:
        match["properties.pwaCode"] = pwa_code
    if pipe_type:
        type_field = "properties.pipeTypeId" if layer == "leakpoint" else "properties.typeId"
        match[type_field] = pipe_type
    if pipe_func_id:
        match["properties.functionId"] = pipe_func_id

    size_field = "$properties.pipeSizesId" if layer == "leakpoint" else "$properties.sizeId"

    if size and not size_gte and not size_lt:
        plain_field = size_field.lstrip("$")
        match[plain_field] = size
    if size_gte and size_lt:
        match["$expr"] = {"$and": [
            {"$gte": [{"$toInt": size_field}, int(size_gte)]},
            {"$lt": [{"$toInt": size_field}, int(size_lt)]},
        ]}
    elif size_gte:
        match["$expr"] = {"$gte": [{"$toInt": size_field}, int(size_gte)]}
    elif size_lt:
        match["$expr"] = {"$lt": [{"$toInt": size_field}, int(size_lt)]}
    if extra:
        match.update(extra)
    return {"$match": match} if match else {"$match": {}}
