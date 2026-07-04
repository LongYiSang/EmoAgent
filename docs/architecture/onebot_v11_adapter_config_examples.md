# OneBot v11 Adapter Config Examples

> MVP scope: private text messages only. Group messages, notices, requests, meta events, media download/upload, HTTP POST, OneBot v12, and vendor extension APIs are intentionally not implemented.

## Reverse WS: NapCat Local

```yaml
platforms:
  enabled: true
  common:
    default_persona: default
    command_prefixes: ["/"]
  adapters:
    qq-main:
      enabled: true
      kind: onebot_v11
      instance_id: qq-main
      platform_id: qq
      config:
        implementation: napcat
        source_type: onebot
        transport:
          mode: ws_reverse
          reverse_path: /api/platforms/onebot/v11/qq-main/ws
          access_token: ""
          access_token_env: ""
          request_timeout_ms: 15000
          connect_timeout_ms: 5000
          reconnect_interval_ms: 3000
        routing:
          private_enabled: true
          group_enabled: false
          ignore_self_messages: true
          private_scope: private
          group_scope: group_shared
        message:
          input_format: array
          output_format: array
          unsupported_segment_policy: placeholder
          max_text_chars: 8000
        outbound:
          auto_escape: false
          coalesce_command_events: true
          split_long_messages: true
          max_message_chars: 1800
```

Configure the OneBot implementation to connect to:

```text
ws://127.0.0.1:8080/api/platforms/onebot/v11/qq-main/ws
X-Client-Role: Universal
X-Self-ID: <bot qq id>
Authorization: Bearer <token>   # only when access_token/access_token_env is set
```

## Forward WS: SnowLuma Local

```yaml
platforms:
  enabled: true
  adapters:
    qq-main:
      enabled: true
      kind: onebot_v11
      instance_id: qq-main
      platform_id: qq
      config:
        implementation: snowluma
        source_type: onebot
        transport:
          mode: ws_client
          url: ws://127.0.0.1:3001/
          access_token_env: ONEBOT_ACCESS_TOKEN
        routing:
          private_enabled: true
          group_enabled: false
        message:
          input_format: array
          output_format: array
          unsupported_segment_policy: placeholder
        outbound:
          coalesce_command_events: true
          split_long_messages: true
          max_message_chars: 1800
```

## Stability Notes

- `source_type` should stay `onebot`.
- `platform_id` should stay `qq`.
- `instance_id` should be a stable logical ID such as `qq-main`.
- Switching `implementation` between `napcat`, `snowluma`, and `generic` does not change OriginKey identity.
- Unsupported inbound segments render as safe placeholders such as `[图片]`; `InboundMessage.Parts` is not populated.
