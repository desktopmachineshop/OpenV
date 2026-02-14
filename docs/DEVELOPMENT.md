## Development Setup

For local development without Docker:

### Backend Development
1. **Install Go 1.21+**
   - Download from [golang.org](https://golang.org)
   - Verify: `go version`

2. **Install PostgreSQL 15+**
   - Download from [postgresql.org](https://postgresql.org)
   - Create database: `createdb openv`
   - Verify: `psql -U postgres -d openv`

3. **Run Backend**
   ```bash
   export DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=openv
   go run cmd/server/main.go
   ```

### Frontend Development
1. **Install Node.js 20+**
   - Download from [nodejs.org](https://nodejs.org)
   - Verify: `node --version` and `npm --version`

2. **Install Dependencies**
   ```bash
   cd frontend
   npm install
   ```

3. **Run Frontend**
   ```bash
   npm start
   ```

### Database Management
```bash
# Connect to database
psql -U postgres -d openv

# View tables
\dt

# View artifact schema
\d artifacts

# View links schema
\d links

# Exit psql
\q
```

## Build for Production

### Build Backend Binary
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/server cmd/server/main.go
```

### Build Frontend
```bash
cd frontend
npm run build
# Output in frontend/build/
```

### Build Docker Images
```bash
docker build -f Dockerfile.api -t openv-api:latest .
docker build -f frontend/Dockerfile -t openv-frontend:latest frontend
```
