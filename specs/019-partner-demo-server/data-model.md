# Data Model: Partner Demo Server

## Entities

### PartnerConfig (simple)
- Minimal config structure compatible with partner.yaml format
- Used to configure port, data paths, auth settings

### AuthToken
- Simple token string returned on successful auth
- Verified by auth verification endpoint

### DemoData
- Channel list data (loaded from file)
- EPG/schedule data (loaded from file)
- Communication data files (served directly)

## File Structure
- Demo data files stored in a configurable directory (e.g., `./data/partner/` or similar)
- Server reads them on startup or serves them directly
