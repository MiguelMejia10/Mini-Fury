FROM node:22-alpine AS frontend
WORKDIR /app
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build
FROM caddy:2.11.7-alpine
COPY --from=frontend /app/dist /srv
COPY infra/production/Caddyfile /etc/caddy/Caddyfile
