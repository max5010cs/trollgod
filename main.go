package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/telebot.v3"
)

var (
	userMemory  = make(map[int64][]string)
	botPaused   = false
	botShutdown = false
	groupList   = make(map[int64]string)
	mu          sync.RWMutex
)

func main() {
	// load .env
	if os.Getenv("LOCAL_ENV") == "1" {
		if err := godotenv.Load(); err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	// Health check to avoid render port scanning 
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
	groqKey := os.Getenv("GROQ_API_KEY")
	ownerUsername := os.Getenv("OWNER_USERNAME")
	ownerInfo := os.Getenv("OWNER_INFO")

	ownerIDStr := os.Getenv("OWNER_ID")
	ownerID, err := strconv.ParseInt(ownerIDStr, 10, 64)
	if err != nil {
		log.Fatal("Invalid OWNER_ID in .env")
	}

	p := telebot.Settings{
		Token:  token,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	bot, err := telebot.NewBot(p)
	if err != nil {
		log.Fatal(err)
	}

	


	// control panel handler
	bot.Handle("/panel", func(c telebot.Context) error {
		msg := c.Message()
		if msg.Chat.Type != telebot.ChatPrivate || msg.Sender.Username != ownerUsername {
			return c.Send("Nice try, but only my boss can use this.")
		}
		panel := &telebot.ReplyMarkup{}
		pauseBtn := panel.Data("Pause", "pause")
		resumeBtn := panel.Data("Resume", "resume")
		groupsBtn := panel.Data("Groups", "groups")
		shutdownBtn := panel.Data("Shutdown", "shutdown")
		panel.Inline(
			panel.Row(pauseBtn, resumeBtn),
			panel.Row(groupsBtn),
			panel.Row(shutdownBtn),
		)
		return c.Send("Control Panel:", panel)
	})

	// Inline button handlers
	bot.Handle(&telebot.Btn{Unique: "pause"}, func(c telebot.Context) error {
		if c.Sender().Username != ownerUsername {
			return c.Send("You wish 💀")
		}
		mu.Lock()
		botPaused = true
		mu.Unlock()
		return c.Send("Bot paused in groups.")
	})
	bot.Handle(&telebot.Btn{Unique: "resume"}, func(c telebot.Context) error {
		if c.Sender().Username != ownerUsername {
			return c.Send("You wish 💀")
		}
		mu.Lock()
		botPaused = false
		mu.Unlock()
		return c.Send("Bot resumed in groups.")
	})
	bot.Handle(&telebot.Btn{Unique: "shutdown"}, func(c telebot.Context) error {
		if c.Sender().Username != ownerUsername {
			return c.Send("You wish 💀")
		}
		mu.Lock()
		botShutdown = !botShutdown
		mu.Unlock()
		if botShutdown {
			return c.Send("Bot shutting down. Bye 👋")
		}
		return c.Send("Bot is back online!")
	})
	bot.Handle(&telebot.Btn{Unique: "groups"}, func(c telebot.Context) error {
		if c.Sender().Username != ownerUsername {
			return c.Send("You wish 💀")
		}
		mu.RLock()
		defer mu.RUnlock()
		if len(groupList) == 0 {
			return c.Send("No groups found.")
		}
		panel := &telebot.ReplyMarkup{}
		var rows []telebot.Row
		for id, title := range groupList {
			btnName := panel.Data(title, fmt.Sprintf("noop_%d", id)) // just label, does nothing
			btnMsg := panel.Data("Message", fmt.Sprintf("msg_%d", id))
			btnLeave := panel.Data("Leave", fmt.Sprintf("leave_%d", id))
			rows = append(rows, panel.Row(btnName, btnMsg, btnLeave))
		}
		panel.Inline(rows...)
		return c.Send("Groups:", panel)
	})
	// leave group handler
	bot.Handle(telebot.OnCallback, func(c telebot.Context) error {
		log.Printf("Callback received: %s from %s", c.Callback().Data, c.Sender().Username)
		if c.Sender().Username != ownerUsername {
			return c.Send("You wish 💀")
		}
		data := c.Callback().Data
		if strings.HasPrefix(data, "leave_") {
			idStr := strings.TrimPrefix(data, "leave_")
			var chatID int64
			fmt.Sscanf(idStr, "%d", &chatID)
			log.Printf("Owner %s triggered leave for group %d", ownerUsername, chatID)
			_, err := bot.Send(&telebot.Chat{ID: chatID}, "Trollgod is out! Group too boring for my taste 💀")
			if err != nil {
				log.Printf("Manual send error: %v", err)
			} else {
				log.Printf("Manual send success")
			}
			err = bot.Leave(&telebot.Chat{ID: chatID})
			if err != nil {
				log.Printf("Manual leave error: %v", err)
			} else {
				log.Printf("Manual leave success")
			}
			return c.Send("Left group.")
		} else if strings.HasPrefix(data, "msg_") {
			idStr := strings.TrimPrefix(data, "msg_")
			var chatID int64
			fmt.Sscanf(idStr, "%d", &chatID)
			log.Printf("Owner %s triggered message for group %d", ownerUsername, chatID)
			_, err := bot.Send(&telebot.Chat{ID: chatID}, "Manual test: Owner triggered this message!")
			if err != nil {
				log.Printf("Manual send error: %v", err)
				return c.Send("Failed to send message.")
			}
			log.Printf("Manual send success")
			return c.Send("Message sent to group.")
		}
		return nil
	})

	// main message handler
	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		msg := c.Message()
		if botShutdown {
			return nil
		}

		// track groups
		if msg.Chat.Type == telebot.ChatGroup || msg.Chat.Type == telebot.ChatSuperGroup {
			mu.Lock()
			groupList[msg.Chat.ID] = msg.Chat.Title
			mu.Unlock()
		}

		// greeting
		if msg.Chat.Type == telebot.ChatPrivate && msg.Sender.Username == ownerUsername {
			return c.Send("Welcome back, boss 👑")
		}

		// pause/resume
		if botPaused && msg.Chat.Type != telebot.ChatPrivate && msg.Sender.Username != ownerUsername {
			return nil
		}

		
		botName := "trollgod"
		isOwner := msg.Sender.Username == ownerUsername
		mentionsBot := strings.Contains(strings.ToLower(msg.Text), botName)
		repliedToBot := msg.ReplyTo != nil && msg.ReplyTo.Sender != nil && msg.ReplyTo.Sender.Username == bot.Me.Username

		// my info 
		ownerQuestions := []string{
			"who is your owner", "who's your owner", "who created you", "your creator", "your owner",
		}
		isPersonal := false
		for _, pq := range ownerQuestions {
			if strings.Contains(strings.ToLower(msg.Text), pq) {
				isPersonal = true
				break
			}
		}
		if isPersonal {
			// only respond if bot name is mentioned or replied to bot
			if mentionsBot || repliedToBot {
				aiResp, err := fetchOwnerInfoAI(groqKey, msg.Text, ownerInfo)
				if err != nil {
					return c.Send("Couldn't fetch owner info right now.")
				}
				return c.Send(aiResp)
			}
			
			return nil
		}

		// default behaviour
		if msg.Chat.Type == telebot.ChatPrivate {
			if msg.Sender.Username != ownerUsername {
				return c.Send("bro I'm not your diary 💀")
			}
			// i get normal responses
			return c.Send("What do you need, boss?")
		}

		// group trlling logic
		if isOwner {
			// owner must mention bot name to get a reply
			if !mentionsBot && !repliedToBot {
				return nil
			}
			// owner mentioned bot name or replied to bot: send friendly/funny AI reply
			systemPrompt := "You are trollgod, a sarcastic but loyal Telegram bot. The person messaging you is your creator and boss. When they mention your name or reply to your message, respond with a funny, friendly, and clever message that shows respect and humor. Never troll or roast your creator. Make them smile. Avoid cringe and AI meta language. Keep it under 15 words."
			trollReply, err := fetchTrollReply(groqKey, msg.Text, msg.Sender.Username, ownerUsername, systemPrompt)
			if err != nil {
				log.Println("error:", err)
				return nil
			}
			if isFiltered(trollReply) {
				log.Println("response:", trollReply)
				return nil
			}
			return c.Send(trollReply)
		} else {
			// non-owner: only respond if bot name is mentioned or replied to bot
			if !mentionsBot && !repliedToBot {
				return nil
			}
			// non-owner mentioned bot name or replied to bot: send troll AI reply
systemPrompt := fmt.Sprintf(
	"You are trollgod, a savage, unpredictable, and foul-mouthed troll in a Telegram group. You roast anyone who replies to you or mentions your name. You're brutal, vulgar, chaotic — a walking insult factory. Always take the side of @%s if they are involved, and humiliate anyone who challenges them. Never use AI meta language or formatting. Keep replies under 15 words. Use insults, slang, and troll lingo.",
	ownerUsername,
)









			trollReply, err := fetchTrollReply(groqKey, msg.Text, msg.Sender.Username, ownerUsername, systemPrompt)
			if err != nil {
				log.Println("error:", err)
				return nil
			}
			if isFiltered(trollReply) {
				log.Println("response:", trollReply)
				return nil
			}
			return c.Send(trollReply)
		}

		// respond if someone replies to a bot message
		repliedToBot = msg.ReplyTo != nil && msg.ReplyTo.Sender != nil && msg.ReplyTo.Sender.Username == bot.Me.Username
		if repliedToBot {
			// extract the original message text
			originalMsg := msg.ReplyTo.Text

			// send the original message text to me
			_, err := bot.Send(&telebot.User{ID: ownerID}, "Message from group:\n"+originalMsg)
			if err != nil {
				log.Println("error sending to owner:", err)
			}
			return nil
		}

		
		return nil
	})

	bot.Start()
}

// my info fetches here
func fetchOwnerInfoAI(apiKey, userMsg, ownerInfo string) (string, error) {
	url := "https://api.groq.com/openai/v1/chat/completions"
	body := map[string]interface{}{
		"model": "llama3-8b-8192",
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "You are a helpful assistant. If asked about your owner, use this info: " + ownerInfo,
			},
			{
				"role":    "user",
				"content": userMsg,
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
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("error: %s", string(bodyBytes))
		return "", fmt.Errorf("error: %s", resp.Status)
	}
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
	return "", fmt.Errorf("unexpected response")
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

func fetchTrollReply(apiKey, latestMsg, sender, owner, systemPrompt string) (string, error) {
	url := "https://api.groq.com/openai/v1/chat/completions"

	body := map[string]interface{}{
		"model": "llama3-8b-8192",
		"messages": []map[string]string{
			{
				"role": "system",
				"content": systemPrompt,
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

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("error: %s", string(bodyBytes))
		return "", fmt.Errorf("error: %s", resp.Status)
	}

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

	return "", fmt.Errorf("unexpected response")
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

// --- MANUAL TEST CODE FOR MESSAGING AND LEAVING ---
// Uncomment and run this block inside main() to manually test sending and leaving:
//
// go func() {
//     chatID := int64(-1002859097966) // Replace with your actual group chat ID
//     _, err := bot.Send(&telebot.Chat{ID: chatID}, "Manual test: Bot will now leave this group!")
//     if err != nil {
//         log.Printf("Manual send error: %v", err)
//     } else {
//         log.Printf("Manual send success")
//     }
//     err = bot.Leave(&telebot.Chat{ID: chatID})
//     if err != nil {
//         log.Printf("Manual leave error: %v", err)
//     } else {
//         log.Printf("Manual leave success")
//     }
// }()
