# syntax=docker/dockerfile:1

FROM golang:1.23

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/reference/dockerfile/#copy
COPY *.go ./
ENV AMQP_URL=amqp://lab3:messageBroker@rabbitmq:5672/
ENV DATABASE_URL=postgresql://user:password@postgres:5432/event_sourcing
ENV SECRET_KEY="my_secret_key"
ENV PORT=5000
# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /lab-3-provider

# Optional:
# To bind to a TCP port, runtime parameters must be supplied to the docker command.
# But we can document in the Dockerfile what ports
# the application is going to listen on by default.
# https://docs.docker.com/reference/dockerfile/#expose
EXPOSE 5000

RUN chmod +x /lab-3-provider

# Run
CMD ["/lab-3-provider"]