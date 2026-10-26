FROM node:22-alpine AS verify
WORKDIR /app
COPY server.mjs server.test.mjs ./
RUN node --test server.test.mjs
FROM node:22-alpine
WORKDIR /app
COPY --from=verify /app/server.mjs ./
USER 10001
EXPOSE 8080
CMD ["node","server.mjs"]
