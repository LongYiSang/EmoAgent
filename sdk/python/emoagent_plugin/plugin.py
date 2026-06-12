import asyncio
import inspect
import os
import sys
import threading
from typing import Any, Awaitable, Callable

from .rpc import RPCPeer
from .types import AttrView

HookHandler = Callable[["Context"], Any | Awaitable[Any]]
ToolHandler = Callable[..., Any | Awaitable[Any]]

_default_plugin: "Plugin | None" = None


class Context:
    def __init__(self, plugin_id: str, rpc: RPCPeer, raw: dict[str, Any] | None = None):
        self.plugin_id = plugin_id
        self.rpc = rpc
        self.raw = raw or {}
        self.envelope = AttrView(self.raw.get("Envelope") or self.raw.get("envelope") or {})
        self.turn = AttrView(self.raw.get("Turn") or self.raw.get("turn") or {})
        self.memory = AttrView(self.raw.get("Memory") or self.raw.get("memory") or {})
        self.tool = AttrView(self.raw.get("Tool") or self.raw.get("tool") or {})
        self.work = AttrView(self.raw.get("Work") or self.raw.get("work") or {})
        self.outbound = AttrView(self.raw.get("Outbound") or self.raw.get("outbound") or {})
        self.config = self.raw.get("Config") or self.raw.get("config") or {}

    async def facade_call(self, method: str, params: dict[str, Any] | None = None) -> Any:
        return await asyncio.to_thread(self.rpc.call, "facade.call", {
            "plugin_id": self.plugin_id,
            "method": method,
            "params": params or {},
        })

    async def provider_generate(self, **params: Any) -> Any:
        return await self.facade_call("provider.generate", params)

    async def kv_get(self, key: str) -> Any:
        return await self.facade_call("plugin.kv.get", {"key": key})

    async def kv_set(self, key: str, value: Any) -> Any:
        return await self.facade_call("plugin.kv.set", {"key": key, "value": value})

    async def log(self, level: str, message: str, fields: dict[str, Any] | None = None) -> None:
        try:
            await self.facade_call("log.emit", {"level": level, "message": message, "fields": fields or {}})
        except Exception:
            print(f"[{level}] {message} {fields or {}}", file=sys.stderr, flush=True)


class Plugin:
    def __init__(self):
        global _default_plugin
        _default_plugin = self
        self.plugin_id = os.environ.get("EMO_PLUGIN_ID", "")
        self._hooks: dict[str, HookHandler] = {}
        self._tools: dict[str, tuple[dict[str, Any], ToolHandler]] = {}
        self._rpc: RPCPeer | None = None

    def hook(self, name: str) -> Callable[[HookHandler], HookHandler]:
        def decorator(func: HookHandler) -> HookHandler:
            self._hooks[name] = func
            return func
        return decorator

    def tool(
        self,
        name: str,
        *,
        description: str = "",
        parameters: dict[str, Any] | None = None,
        scope: str = "both",
        permission: str = "read-only",
    ) -> Callable[[ToolHandler], ToolHandler]:
        def decorator(func: ToolHandler) -> ToolHandler:
            self._tools[name] = ({
                "name": name,
                "description": description,
                "parameters": parameters or {"type": "object"},
                "scope": scope,
                "permission": permission,
            }, func)
            return func
        return decorator

    def run_stdio(self) -> None:
        self._rpc = RPCPeer(self._handle)
        self._rpc.serve_forever()

    def _handle(self, method: str, params: dict[str, Any]) -> Any:
        if method == "initialize":
            self.plugin_id = params.get("plugin_id") or self.plugin_id
            return {"tools": [spec for spec, _ in self._tools.values()]}
        if method == "invoke_hook":
            return self._invoke_hook(params)
        if method == "invoke_tool":
            return self._invoke_tool(params)
        if method == "health":
            return {"ok": True}
        if method == "shutdown":
            threading.Timer(0.01, lambda: os._exit(0)).start()
            return None
        raise RuntimeError(f"unsupported method {method}")

    def _invoke_hook(self, params: dict[str, Any]) -> Any:
        name = str(params.get("hook", ""))
        handler = self._hooks.get(name)
        if handler is None:
            return {}
        ctx = Context(self.plugin_id, self._require_rpc(), params.get("context") or {})
        return _run(handler(ctx)) or {}

    def _invoke_tool(self, params: dict[str, Any]) -> Any:
        name = str(params.get("tool", ""))
        record = self._tools.get(name)
        if record is None:
            raise RuntimeError(f"tool {name} is not registered")
        _, handler = record
        input_data = params.get("input") or {}
        ctx = Context(self.plugin_id, self._require_rpc(), params)
        if len(inspect.signature(handler).parameters) >= 2:
            return _run(handler(input_data, ctx))
        return _run(handler(input_data))

    def _require_rpc(self) -> RPCPeer:
        if self._rpc is None:
            raise RuntimeError("rpc peer is not running")
        return self._rpc


def hook(name: str) -> Callable[[HookHandler], HookHandler]:
    if _default_plugin is None:
        Plugin()
    return _default_plugin.hook(name)  # type: ignore[union-attr]


def tool(
    name: str,
    *,
    description: str = "",
    parameters: dict[str, Any] | None = None,
    scope: str = "both",
    permission: str = "read-only",
) -> Callable[[ToolHandler], ToolHandler]:
    if _default_plugin is None:
        Plugin()
    return _default_plugin.tool(name, description=description, parameters=parameters, scope=scope, permission=permission)  # type: ignore[union-attr]


def _run(value: Any) -> Any:
    if inspect.isawaitable(value):
        return asyncio.run(value)
    return value
