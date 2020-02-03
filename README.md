# Distributed Web-Crawler in Golang

A basic webcrawler that hunts for all `<img>` tags on a domain and saves their `src` attributes to a Redis key set.

Run multiple crawler containers with:
```
docker-compose up --scale crawer=5
```

Edit the `docker-compose.yml` file to adjust concurrency (goroutines) per container, the target URL and other such env-vars.