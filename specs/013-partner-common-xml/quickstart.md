# Quickstart: Partner Common XML Source

Validation guide for the feature. Contracts live in [contracts/common-xml-api.md](./contracts/common-xml-api.md) and [contracts/partner-config.md](./contracts/partner-config.md); entity rules live in [data-model.md](./data-model.md).

## Prerequisites

- Go 1.26+ and `make`.
- PostgreSQL and Redis reachable via `config/.env`.
- Access to the partner API used by the deployment.
- `curl` and a standard XML parser (`xmllint`, `python3 -c "import xml..."`, etc.).

## 1. Configure the partner

1. Copy `config/partner.example.yaml` to `config/partner.yaml`.
2. Set `active` to the profile you want to serve.
3. Fill `partners.<id>.api.base_url` and endpoint paths for the partner.
4. Move the existing `auth.yaml` `verify` and `steps` blocks under `partners.<id>.auth`.
5. Remove `config/auth.yaml` once the new file is in place.

Expected: no `config/auth.yaml` is required.

## 2. Build and run

```bash
make build
make start-api
```

To expose the API on the example port, set `API_ADDR=:3030` in the environment before starting (or bind the deployment host as appropriate). Default is `:8080`.

Health check:

```bash
curl -i http://localhost:8080/favicon.ico
```

## 3. Channel list XML

```bash
curl -i http://localhost:8080/common/channel-playouts-xml
```

Expected:

- HTTP 200 and `Content-Type: application/xml; charset=utf-8`.
- Well-formed XML with root `<channel-playouts>`.
- Every `<channel>` has `<id>`, `<name>`, `<img>` in that order.
- A channel with no upstream image has an empty `<img></img>`.

Parse check:

```bash
curl -s http://localhost:8080/common/channel-playouts-xml | xmllint --noout -
```

## 4. EPG XML

```bash
curl -i "http://localhost:8080/common/channel/epg/<channelId>"
curl -i "http://localhost:8080/common/channel/epg/<channelId>?\$include_metadata=true"
curl -i "http://localhost:8080/common/channel/epg/<channelId>?date=2026-11-02"
```

Expected:

- Root `<epg>` with one `<channelId>` child.
- Each `<epgitem>` has `<start>`, `<stop>`, `<streamId>`, `<ads>` in that order.
- `start`/`stop` are RFC3339 with an explicit offset.
- `ads` is `true` only for programmes flagged as ads in the EPG.
- Metadata-only custom fields appear with `$include_metadata=true` and are absent otherwise.
- A valid channel with no schedule returns 200 and no `<epgitem>` children.

Error checks:

```bash
# 404 + <error>
curl -i "http://localhost:8080/common/channel/epg/does-not-exist"

# 400 + <error>
curl -i "http://localhost:8080/common/channel/epg/..%2Fetc"

# 400 + <error> for an invalid date
curl -i "http://localhost:8080/common/channel/epg/<channelId>?date=not-a-date"
```

## 5. Partner customisation

1. Add a custom channel field to the active profile:

```yaml
xml:
  channel:
    custom_fields:
      - name: "provider"
        value: "partner-a"
```

2. Re-request `/common/channel-playouts-xml` without restarting.

Expected: `<provider>partner-a</provider>` appears after `<img>` for each channel, and the base elements are unchanged.

3. Add an invalid field (missing `source` and `value`, or a reserved name) and re-request.

Expected: the field is absent, the base XML is still valid, and the API log reports the configuration problem.

## 6. Auth configuration still works after migration

```bash
curl -s http://localhost:8080/api/auth/config
```

Expected: the same `verify` and `steps` JSON shape as before, sourced from the active profile's `auth` block. Temporarily rename `partner.yaml` to confirm the fallback path still returns a usable login flow and logs the degradation.

## 7. Ads come from the EPG

Use a channel whose schedule contains explicit ad programmes.

1. Prepare the channel (start it or wait for the planner tick).
2. Inspect the running timeline:

```bash
curl -s "http://localhost:8080/api/channels/<channelId>/segment-track?limit=200"
```

Expected:

- Entries from EPG ad programmes have ad markers (`is_ad`, `ad_type`, `ad_break_id`, `ad_break_start`, `ad_break_duration`).
- No ad entry exists solely because a fixed interval elapsed.
- The EPG XML marks those programmes with `<ads>true</ads>` and the ad stream id.

3. Confirm the setting is gone from the environment and logs:

```bash
grep -R "AD_INSERTION_INTERVAL" config .env.example 2>/dev/null
```

Expected: no occurrences.

## 8. Automated checks

```bash
make fmt
make vet
make test
```

Focused runs while developing:

```bash
go test ./internal/partner/...
go test ./internal/api/ -run 'Partner|Auth'
go test ./internal/planner/ -run 'Ad'
```

Expected: all pass, including the new config loader, XML builder, handler, and EPG ad-marking tests.

## 9. Cleanup

```bash
make stop
```

If you changed `config/partner.yaml` for local testing, restore or remove the file before committing; it is gitignored and should not be committed.
