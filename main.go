package main

import (
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/go-redis/redis/v7"

	"github.com/PonyFest/auction-bot/api"
	"github.com/PonyFest/auction-bot/auction"
	"github.com/PonyFest/auction-bot/bot"
)

type config struct {
	redisURL string
	discordToken string
	discordChannel string
	apiPassword string
	bind string
}

func parseConfig() (config, error) {
	c := config{}
	flag.StringVar(&c.redisURL, "redis-url", "", "URL of the redis database")
	flag.StringVar(&c.discordToken, "discord-token", "", "Discord bot auth token")
	flag.StringVar(&c.discordChannel, "discord-channel", "", "ID of the auction discord channel")
	flag.StringVar(&c.apiPassword, "api-password", "", "The password required to hit the HTTP API")
	flag.StringVar(&c.bind, "bind", "0.0.0.0:8080", "The address:port to bind the HTTP API to.")
	flag.Parse()

	if c.redisURL == "" {
		return c, errors.New("--redis-url is required")
	}
	if c.discordToken == "" {
		return c, errors.New("--discord-token is required")
	}
	if c.discordChannel == "" {
		return c, errors.New("--discord-channel is required")
	}
	return c, nil
}

func main() {
	c, err := parseConfig()
	if err != nil {
		log.Fatalf("invalid arguments: %v.\n", err)
	}
	r, err := getRedisClient(c.redisURL)
	if err != nil {
		log.Fatalf("couldn't get redis client: %v.\n", err)
	}
	a := auction.New(r)
	b, err := bot.New(a, c.discordToken, c.discordChannel)
	if err != nil {
		log.Fatalf("couldn't create bot: %v.\n", err)
	}
	go b.RunForever()
	server := api.New(a, c.apiPassword)
	log.Fatalln(server.ListenAndServe(c.bind))
}


func getRedisClient(url string) (*redis.Client, error) {
	redisOptions, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL %q: %v", url, err)
	}
	return redis.NewClient(redisOptions), nil
}