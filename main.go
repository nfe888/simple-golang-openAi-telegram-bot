package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	openAIAPI = "https://api.openai.com/v1/chat/completions"
)

var ctx = context.Background()

var openAiToken, telegramBotToken string

type Update struct {
	UpdateID int `json:"update_id"`
	Message  struct {
		MessageID int `json:"message_id"`
		From      struct {
			ID        int    `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			UserName  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID    int    `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

type OpenAiResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

var redisClient *redis.Client

func main() {

	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Some error occured on reading . env file. Err: %s", err)
	}

	openAiToken = os.Getenv("openAiToken")
	telegramBotToken = os.Getenv("telegramBotToken")

	redisClient = redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: os.Getenv("redisPassword"),
		DB:       0,
	})

	setTelegramWebhook()

	router := mux.NewRouter()
	router.HandleFunc("/webhook", handleWebhook).Methods("POST")

	http.ListenAndServe(":"+os.Getenv("golangPort"), router)
}

func setTelegramWebhook() {
	setWebhookReqBody := map[string]interface{}{
		"url": os.Getenv("botDomain"),
	}
	sendTelegramRequest(setWebhookReqBody, "setWebhook")
}

func sendTelegramRequest(reqBody map[string]interface{}, endPoint string) {
	client := &http.Client{}
	reqBodyJSON, _ := json.Marshal(reqBody)
	log.Println(string(reqBodyJSON))
	request, _ := http.NewRequest("POST", "https://api.telegram.org/bot"+telegramBotToken+"/"+endPoint, bytes.NewBuffer(reqBodyJSON))
	request.Header.Set("Content-Type", "application/json")
	sendMessageResp, _ := client.Do(request)
	defer sendMessageResp.Body.Close()
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := ioutil.ReadAll(r.Body)

	var update Update

	json.Unmarshal(body, &update)

	if update.Message.Text == "" {
		return
	}
	text := ""
	if strings.ToUpper(update.Message.Text) == "/START" {
		text = "Welcome, You can start chatting with Haj Jipit."
	} else if strings.ToUpper(update.Message.Text) == "/CLEAR" {
		redisClient.Del(ctx, "userHistory:"+strconv.Itoa(update.Message.Chat.ID))
		text = "Conversation cleared."
	} else {

		messages := getMessagesFromRedis(update.Message.Chat.ID)
		var newMessage = Message{Content: update.Message.Text, Role: "user"}
		messages = append(messages, newMessage)

		openAIResponse := callOpenAiApi(messages)

		if len(openAIResponse.Choices) > 0 {
			text = openAIResponse.Choices[0].Message.Content
			messageJson, _ := json.Marshal(newMessage)
			replyJson, _ := json.Marshal(openAIResponse.Choices[0].Message)
			redisClient.RPush(ctx, "userHistory:"+strconv.Itoa(update.Message.Chat.ID), messageJson, replyJson)
		} else {
			text = "There was a problem processing your message. Maybe it's because of number of tokens. Try to /CLEAR your conversation history."
		}

	}
	sendTelegramMessage(update.Message.Chat.ID, text)

}

func callOpenAiApi(messages []Message) OpenAiResponse {
	client := &http.Client{}

	openAIReqBody := map[string]interface{}{
		"model":    "gpt-3.5-turbo",
		"messages": messages,
	}
	openAIReqBodyJSON, _ := json.Marshal(openAIReqBody)
	openAIReq, _ := http.NewRequest("POST", openAIAPI, bytes.NewBuffer(openAIReqBodyJSON))
	openAIReq.Header.Set("Content-Type", "application/json")
	openAIReq.Header.Set("Authorization", "Bearer "+openAiToken)

	openAIResp, _ := client.Do(openAIReq)
	openAIRespBody, _ := ioutil.ReadAll(openAIResp.Body)
	defer openAIResp.Body.Close()
	log.Printf("openAi response %+v", string(openAIRespBody))

	var openAIResponse OpenAiResponse
	json.Unmarshal(openAIRespBody, &openAIResponse)

	return openAIResponse

}

func getMessagesFromRedis(chatID int) []Message {
	list := redisClient.LRange(ctx, "userHistory:"+strconv.Itoa(chatID), 0, -1)
	var messages []Message
	for _, value := range list.Val() {
		var message Message
		json.Unmarshal([]byte(value), &message)
		messages = append(messages, message)
	}
	return messages
}

func sendTelegramMessage(chatID int, text string) {
	sendMessageReqBody := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}
	sendTelegramRequest(sendMessageReqBody, "sendMessage")
}
