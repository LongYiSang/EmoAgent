import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))

from emoagent_plugin import Plugin, hook, tool

plugin = Plugin()


@hook("after_turn_end")
async def after_turn_end(ctx):
    await ctx.log("info", "turn ended", {"turn_id": ctx.turn.turn_id})
    await ctx.kv_set("last_turn_id", ctx.turn.turn_id)
    return {"Annotations": {"echo_plugin": f"observed:{ctx.turn.turn_id}"}}


@tool(
    "echo",
    description="Echo input through the plugin process",
    parameters={
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"],
    },
    scope="both",
    permission="read-only",
)
async def echo(input_data):
    return {"ok": True, "text": input_data.get("text", "")}


@tool(
    "provider_ping",
    description="Call EmoAgent ProviderGateway",
    parameters={"type": "object", "properties": {"text": {"type": "string"}}},
    scope="both",
    permission="read-only",
)
async def provider_ping(input_data, ctx):
    return await ctx.provider_generate(
        purpose="example",
        messages=[{"role": "user", "content": input_data.get("text", "ping")}],
        max_tokens=64,
    )


if __name__ == "__main__":
    plugin.run_stdio()
