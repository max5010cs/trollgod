package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/telebot.v3"
)

var userMemory = make(map[int64][]string)

func main() {
	// toggle .env loading based on local env
	if os.Getenv("LOCAL_ENV") == "1" {
		if err := godotenv.Load(); err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	// Start health check server in a goroutine
	go func() {
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})
		log.Println("Health check server running on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatal(err)
		}
	}()

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

		if msg.Chat.Type == telebot.ChatPrivate {
			return c.Send("sorry, not interested")
		}

		if len(userMemory[msg.Sender.ID]) >= 10 {
			userMemory[msg.Sender.ID] = userMemory[msg.Sender.ID][1:]
		}
		userMemory[msg.Sender.ID] = append(userMemory[msg.Sender.ID], msg.Sender.Username+": "+msg.Text)

		trollReply, err := fetchTrollReply(openRouterKey, msg.Text, msg.Sender.Username, ownerUsername)
		if err != nil {
			log.Println("OpenRouter error:", err)
			return nil
		}

		if isFiltered(trollReply) {
			log.Println("Filtered response:", trollReply)
			return nil
		}

		return c.Send(trollReply)
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
		return joined[len(joined)-1000:]
	}
	return joined
}

func fetchTrollReply(apiKey, latestMsg, sender, owner string) (string, error) {
	url := "https://openrouter.ai/api/v1/chat/completions"

	body := map[string]interface{}{
		"model": "mistralai/mistral-small-3.2-24b-instruct:free",
		"messages": []map[string]string{
			{
				"role": "system",
				"content": fmt.Sprintf(
					"You are a sarcastic, chaotic troll god in a Telegram group chat. Always address users directly by their username (e.g. @%s). Be sharp, funny, unpredictable, and always take the side of @%s. Roast others with wit, not cringe. Avoid obvious AI phrases or role labels. No <User>, <Assistant>, or formatting. Make it feel like a human trolling in real time. If someone insults you or @%s, respond mockingly. Be clever and bold, not cartoonish. Your response should be less than 15 words, and always feel like it's replying to the specific person.",
					sender, owner, owner,
				),
			},
			{
				"role":    "user",
				"content": latestMsg,
			},
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
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", err
	}

	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		msg := choices[0].(map[string]interface{})
		if message, ok := msg["message"].(map[string]interface{}); ok {
			if content, ok := message["content"].(string); ok {
				return content, nil
			}
		}
	}

	return "", fmt.Errorf("unexpected OpenRouter response")
}

func isFiltered(reply string) bool {
	lower := strings.ToLower(reply)
	return strings.Contains(lower, "i'm an ai") ||
		strings.Contains(lower, "as an ai") ||
		strings.Contains(lower, "as a language model") ||
		strings.Contains(lower, "i cannot") ||
		strings.Contains(lower, "i'm unable") ||
		strings.Contains(lower, "the user said")
}
