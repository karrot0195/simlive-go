# Research: Admin-Only Timeline Screen

## Decisions

- Use `import.meta.env.VITE_ADMIN_ID` with default `admin@gmail.com`.
- Compare current user email case-insensitively.
- Hide timeline link in navigation for non-admins and add a route guard to block direct access.
- No backend role changes; client-side gate only.