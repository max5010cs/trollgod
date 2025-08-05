package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/telebot.v3"
)

var userMemory = make(map[int][]string)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	openRouterKey := os.Getenv("OPENROUTER_API_KEY")
	ownerUsername := os.Getenv("OWNER_USERNAME")

	p := telebot.Settings{
		Token:  token,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	bot, err := telebot.NewBot(p)
	if err != nil {
		log.Fatal(err)
	}

	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		msg := c.Message()

		// 1-on-1 chat rejection
		if msg.Chat.Type == telebot.ChatPrivate {
			return c.Send("sorry, not interested")
		}

		// store memory per user (max 10 lines)
		if len(userMemory[msg.Sender.ID]) >= 10 {
			userMemory[msg.Sender.ID] = userMemory[msg.Sender.ID][1:]
		}
		userMemory[msg.Sender.ID] = append(userMemory[msg.Sender.ID], msg.Text)

		// respond if user is me or message involves me
		shouldRespond := false
		if msg.Sender.Username == ownerUsername {
			shouldRespond = true
		} else if msg.ReplyTo != nil && msg.ReplyTo.Sender.Username == ownerUsername {
			shouldRespond = true
		} else if strings.Contains(msg.Text, "@"+ownerUsername) {
			shouldRespond = true
		}

		if shouldRespond {
			context := buildMemoryContext(msg.Chat.ID)
			trollReply, err := fetchTrollReply(openRouterKey, context)
			if err != nil {
				log.Println("OpenRouter error:", err)
				return nil
			}
			return c.Send(trollReply)
		}

		return nil
	})

	bot.Start()
}

func buildMemoryContext(chatID int64) string {
	var context []string
	for _, lines := range userMemory {
		context = append(context, lines...)
	}
	joined := strings.Join(context, "\n")
	if len(joined) > 1000 {
		return joined[len(joined)-1000:] //  i can cut if it is too long
	}
	return joined
}

func fetchTrollReply(apiKey, context string) (string, error) {
	url := "https://openrouter.ai/api/v1/chat/completions"
	
	body := map[string]interface{}{
		"model": "mistral/troll-bot", // model
		"messages": []map[string]string{
			{"role": "system", "content": "You are a trolling god in a group chat. Be funny, chaotic, and always side with @levelup1853."},
			{"role": "user", "content": context},
		},
		"max_tokens": 100,
	}
	
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(b))
	req.Header.Add("Authorization", "Bearer "+apiKey)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		msg := choices[0].(map[string]interface{})
		if message, ok := msg["message"].(map[string]interface{}); ok {
			return message["content"].(string), nil
		}
	}

	return "", fmt.Errorf("unexpected OpenRouter response")
}
