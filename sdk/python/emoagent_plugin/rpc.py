import json
import queue
import sys
import threading
from typing import Any, Callable


class RPCPeer:
    def __init__(self, handler: Callable[[str, dict[str, Any]], Any]):
        self._handler = handler
        self._write_lock = threading.Lock()
        self._pending: dict[str, queue.Queue[dict[str, Any]]] = {}
        self._pending_lock = threading.Lock()
        self._next_id = 0
        self._closed = threading.Event()

    def serve_forever(self) -> None:
        for line in sys.stdin:
            if not line.strip():
                continue
            try:
                message = json.loads(line)
            except Exception as exc:
                print(f"emoagent_plugin rpc decode error: {exc}", file=sys.stderr, flush=True)
                continue
            if "method" in message:
                threading.Thread(target=self._handle_request, args=(message,), daemon=True).start()
            else:
                request_id = str(message.get("id", ""))
                with self._pending_lock:
                    pending = self._pending.get(request_id)
                if pending is not None:
                    pending.put(message)
        self._closed.set()

    def call(self, method: str, params: dict[str, Any] | None = None, timeout: float | None = None) -> Any:
        self._next_id += 1
        request_id = str(self._next_id)
        pending: queue.Queue[dict[str, Any]] = queue.Queue(maxsize=1)
        with self._pending_lock:
            self._pending[request_id] = pending
        try:
            self._write({"jsonrpc": "2.0", "id": request_id, "method": method, "params": params or {}})
            response = pending.get(timeout=timeout)
        finally:
            with self._pending_lock:
                self._pending.pop(request_id, None)
        if response.get("error"):
            raise RuntimeError(str(response["error"].get("message", response["error"])))
        return response.get("result")

    def _handle_request(self, request: dict[str, Any]) -> None:
        request_id = request.get("id")
        try:
            result = self._handler(str(request.get("method", "")), request.get("params") or {})
            if request_id is not None:
                self._write({"jsonrpc": "2.0", "id": request_id, "result": result})
        except SystemExit:
            raise
        except Exception as exc:
            if request_id is not None:
                self._write({"jsonrpc": "2.0", "id": request_id, "error": {"code": -32000, "message": str(exc)}})

    def _write(self, value: dict[str, Any]) -> None:
        with self._write_lock:
            sys.stdout.write(json.dumps(value, separators=(",", ":")) + "\n")
            sys.stdout.flush()
