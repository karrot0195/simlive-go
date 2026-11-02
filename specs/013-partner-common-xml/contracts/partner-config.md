# Contract: `config/partner.yaml`

One deployment activates one partner profile. The file is read per request, so edits take effect without a restart. `PARTNER_CONFIG_PATH` may point at an alternate file (used by tests and non-standard deployments).

## Schema

```yaml
active: default

partners:
  default:
    api:
      base_url: "http://localhost:3030"
      timeout: "10s"
      headers:
        Accept: "application/json"
      channel_list_query:
        "$limit": "500"
        "is_playout_assigned": "true"
      schedule_query:
        "$eager": "[programme.[programmeStreams]]"
        "$skip": "0"
        "$limit": "30000"

    auth:
      verify:
        url: "https://api2.example.com/api/v2/accounts/auth/verify-token"
        method: POST
        headers:
          Authorization: "{{token}}"
        clear_on_error: true
      steps:
        - title: "Login"
          fields:
            - name: username
              type: text
              label: Username
              required: true
            - name: password
              type: password
              label: Password
              required: true
          api: "https://msvc.example.com/sso/auth/login"
          method: POST
          headers:
            Content-Type: "application/json"
          body:
            username: "{{username}}"
            password: "{{password}}"
          response_field: "access_token"

    xml:
      channel:
        custom_fields:
          - name: "base_name"
            source: "base_name"
          - name: "provider"
            value: "partner-a"
      epg:
        custom_fields:
          - name: "title"
            source: "programme.name"
            when: "metadata"
          - name: "episode"
            source: "programme.episode"
            when: "metadata"
            default: ""
```

## Field reference

| Path | Required | Notes |
|------|----------|-------|
| `active` | yes | Must name a key under `partners`. |
| `partners` | yes | Map of profile id → profile. |
| `partners.<id>.api` | yes | Upstream connection and endpoints. |
| `partners.<id>.api.base_url` | no | Falls back to `PARTNER_API_URL`, then `http://localhost:3030`. |
| `partners.<id>.api.timeout` | no | Go duration string; default `10s`. |
| `partners.<id>.api.headers` | no | Added to every upstream request. |
| `partners.<id>.api.channel_list_query` | no | Extra query parameters for the channel list call. |
| `partners.<id>.api.schedule_query` | no | Fixed query parameters; `$date` and `tz` are always added. |
| `partners.<id>.auth` | no | Served by `GET /api/auth/config`. Must contain `steps` to be usable. |
| `partners.<id>.xml.channel.custom_fields` | no | Extends the channel base elements. |
| `partners.<id>.xml.epg.custom_fields` | no | Extends the EPG item base elements. |

### Custom field

| Key | Required | Rules |
|-----|----------|-------|
| `name` | yes | Valid XML name, unique, not reserved (`id`, `name`, `img`, `start`, `stop`, `streamId`, `ads`, `channelId`). |
| `source` | one of `source`/`value` | Dotted path; see available sources below. |
| `value` | one of `source`/`value` | Literal text for every item. |
| `when` | no | `always` (default) or `metadata` (requires `$include_metadata=true`). |
| `default` | no | Text used when `source` is missing; default empty. |

## Available `source` paths

### Channel items

| Path | Meaning |
|------|---------|
| `id`, `name`, `img` | Normalized base values. |
| `<upstream field>` | Any top-level field from the partner channel record (e.g. `base_name`, `content_timezone`). |
| `<upstream object>.<field>` | Nested values (e.g. `logo.url`). |

### EPG items

| Path | Meaning |
|------|---------|
| `start`, `stop`, `streamId`, `ads`, `channelId` | Normalized base values. |
| `programme.<field>` | Programme object fields (`name`, `id`, `short_url`, `is_ads`). |
| `programme.programmeStreams.<n>.<field>` | Indexed stream fields when needed. |
| `<schedule field>` | Any top-level schedule item field (`type`, `duration`, `local_date_start`, `local_date_end`). |

## Migration from `config/auth.yaml`

1. Move the existing top-level `verify` and `steps` blocks unchanged under `partners.<id>.auth`.
2. Point `active` at that profile.
3. Remove `config/auth.yaml`.

`GET /api/auth/config` behavior is unchanged. If `partner.yaml` is absent or its auth block is unusable, the loader falls back to `config/auth.yaml` if present, then to the built-in default.

## Validation and degradation

- Missing or unparseable file: log the error; auth falls back as above; XML endpoints return 502 because no partner API is configured.
- `active` does not resolve: log the error; auth falls back; XML endpoints return 502.
- Invalid custom field (bad name, collision, missing `source` and `value`): log and skip that field; base XML is still served.
- Empty `img` source: `<img></img>` is emitted.

## Git hygiene

`config/partner.yaml` is deployment-specific and is added to `config/.gitignore`. `config/partner.example.yaml` is committed with the schema above and placeholder values.
