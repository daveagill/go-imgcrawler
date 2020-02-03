FROM golang:alpine

WORKDIR /app
COPY . .

RUN go build -o crawlsvc ./cmd/crawlsvc

ENV url=""
ENV redis_addr=""
ENV concurrency="5"
CMD ./crawlsvc -url ${url} -redisAddr ${redis_addr} -workers ${concurrency}