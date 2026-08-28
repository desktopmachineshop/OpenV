# Base image policy: pin to specific minor-version tags (not `latest` or
# bare-major tags, not sha digests). Pinned tags still receive OS-package
# security rebuilds from upstream; bump versions here deliberately.
FROM node:20.20-alpine

WORKDIR /app

COPY package*.json ./

# npm ci installs exactly what package-lock.json pins (and fails on drift),
# so every dev image build resolves the same dependency tree.
RUN npm ci --legacy-peer-deps

COPY . .

# Accept API URL as build argument
ARG REACT_APP_API_URL=http://localhost:8080
ENV REACT_APP_API_URL=$REACT_APP_API_URL

EXPOSE 3000

CMD ["sh", "-c", "npm start"]
