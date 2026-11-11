# The costing component alone: a static binary on Alpine (the health check runs through /bin/sh and wget).
FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /erp-costing .

FROM alpine:3.24.2
COPY --from=build /erp-costing /erp-costing
EXPOSE 8229 9229
ENTRYPOINT ["/erp-costing"]
