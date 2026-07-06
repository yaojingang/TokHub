FROM node:25-alpine AS web-build
WORKDIR /src
COPY package.json package-lock.json* tsconfig.json vite.config.ts ./
COPY web ./web
RUN npm ci
RUN npm run build

FROM golang:1.26-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tokhub ./cmd/tokhub

FROM scratch
WORKDIR /app
COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /out/tokhub /app/tokhub
COPY --from=web-build /src/web/dist /app/web
COPY db /app/db
COPY docs /app/docs
ENV TOKHUB_STATIC_DIR=/app/web
ENV TOKHUB_DOCS_DIR=/app/docs
ENV TOKHUB_MIGRATIONS_DIR=/app/db/migrations
EXPOSE 8080
ENTRYPOINT ["/app/tokhub"]
