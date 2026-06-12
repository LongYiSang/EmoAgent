from typing import Any


class AttrView:
    def __init__(self, data: dict[str, Any] | None = None):
        self._data = data or {}

    def __getattr__(self, name: str) -> Any:
        for key in (name, _snake_to_pascal(name), _snake_to_go(name), _snake_to_camel(name)):
            if key in self._data:
                value = self._data[key]
                if isinstance(value, dict):
                    return AttrView(value)
                return value
        return ""

    def raw(self) -> dict[str, Any]:
        return dict(self._data)


def _snake_to_pascal(value: str) -> str:
    return "".join(part.capitalize() for part in value.split("_"))


def _snake_to_camel(value: str) -> str:
    pascal = _snake_to_pascal(value)
    return pascal[:1].lower() + pascal[1:]


def _snake_to_go(value: str) -> str:
    initialisms = {"id": "ID", "url": "URL", "json": "JSON", "api": "API", "db": "DB"}
    return "".join(initialisms.get(part, part.capitalize()) for part in value.split("_"))
