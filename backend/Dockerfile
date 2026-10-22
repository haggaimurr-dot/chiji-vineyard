FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/chiji .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai PORT=8080
COPY --from=build /out/chiji /usr/local/bin/chiji
EXPOSE 8080
USER nobody
ENTRYPOINT ["/usr/local/bin/chiji"]

