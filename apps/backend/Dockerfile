FROM golang:1.25-alpine

RUN apk add --no-cache gcc musl-dev
RUN go install github.com/air-verse/air@latest

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Default is the web process for local development.
# Worker containers override this command from docker-compose or deployment settings.
CMD ["air", "-c", ".air.toml"]
