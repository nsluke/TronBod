FROM golang:1.26-alpine AS build
WORKDIR /src
COPY sync/go.mod sync/go.sum* ./
RUN go mod download
COPY sync/ ./
RUN CGO_ENABLED=0 go build -o /out/sync .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/sync /app/sync
EXPOSE 8090
ENTRYPOINT ["/app/sync"]
