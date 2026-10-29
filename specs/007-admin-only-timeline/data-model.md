# Data Model: Admin Access

## Current User

- email: string (from auth store)

## Admin Email

- value: string from `VITE_ADMIN_ID` (default `admin@gmail.com`)

## Access Rule

- isAdmin = lowercase(currentUser.email) == lowercase(VITE_ADMIN_ID)
- If admin email empty/unset → isAdmin = false for all users.