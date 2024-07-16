package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type QueueItem struct {
	Username  string
	UserID    int
	MessageID int // Store the message ID of the join command
}

var queues = make(map[int64][]QueueItem)
var activeCountdowns = make(map[int64]time.Time)
var countdownOwners = make(map[int64]int)
var mu sync.Mutex

func main() {
	bot, err := tgbotapi.NewBotAPI("7430878489:AAHPTQWwgliaE7J45N7CZkoYxwN-UUhj42c")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	go func() {
		for update := range updates {
			if update.Message == nil {
				continue
			}

			if update.Message.IsCommand() {
				switch update.Message.Command() {
				case "start":
					handleHelp(bot, update.Message)
				case "join":
					handleJoin(bot, update.Message)
				case "stoptime":
					handleStopTime(bot, update.Message)
				case "queue":
					handleQueue(bot, update.Message)
				case "remove":
					handleRemove(bot, update.Message)
				case "help":
					handleHelp(bot, update.Message)
				default:
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Я не знаю такую команду, вы можете воспользоваться /help чтобы посмотреть список поддерживаемых команд.")
					bot.Send(msg)
				}
			}
		}
	}()

	// Start HTTP server to listen on port provided by Heroku
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("$PORT must be set")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Bot is running.")
	})

	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleJoin(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	mu.Lock()
	defer mu.Unlock()

	chatID := message.Chat.ID
	userID := message.From.ID

	// Check if the user is already in the queue
	for _, item := range queues[chatID] {
		if item.UserID == userID {
			msg := tgbotapi.NewMessage(chatID, "Вы уже в очереди.")
			msg.ReplyMarkup = getCommandButtons()
			bot.Send(msg)
			return
		}
	}

	queues[chatID] = append(queues[chatID], QueueItem{
		Username:  message.From.FirstName + " (@" + message.From.UserName + ")",
		UserID:    userID,
		MessageID: message.MessageID, // Store the join command message ID
	})

	response := fmt.Sprintf("%s занял очередь.", message.From.FirstName)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)

	// Automatically start the timer if the queue was empty and no active countdown
	if len(queues[chatID]) == 1 && activeCountdowns[chatID].IsZero() {
		startNextUser(bot, chatID)
	}
}

func startNextUser(bot *tgbotapi.BotAPI, chatID int64) {
	if len(queues[chatID]) == 0 {
		return
	}

	firstInQueue := queues[chatID][0]
	queues[chatID] = queues[chatID][1:]
	countdownOwners[chatID] = firstInQueue.UserID
	activeCountdowns[chatID] = time.Now().Add(10 * time.Minute)

	startTime := time.Now()
	endTime := startTime.Add(10 * time.Minute)

	var nextInQueueMessage string
	var replyToMessageID int
	if len(queues[chatID]) > 0 {
		nextInQueue := queues[chatID][0]
		nextInQueueMessage = fmt.Sprintf("Следующий в очереди: %s", nextInQueue.Username)
		replyToMessageID = nextInQueue.MessageID // Get the message ID of the next user's join command
	} else {
		nextInQueueMessage = "Очередь пуста."
		replyToMessageID = firstInQueue.MessageID // Reply to the first user's join command
	}

	response := fmt.Sprintf("%s начал отсчёт времени.\nПромежуток: %s - %s\n%s", firstInQueue.Username, startTime.Format("15:04:05"), endTime.Format("15:04:05"), nextInQueueMessage)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	msg.ReplyToMessageID = replyToMessageID // Set the reply to the correct message ID
	bot.Send(msg)

	time.AfterFunc(10*time.Minute, func() {
		mu.Lock()
		defer mu.Unlock()

		if activeCountdowns[chatID].After(time.Now()) {
			// Timer was stopped early
			return
		}

		delete(activeCountdowns, chatID)
		delete(countdownOwners, chatID)

		if len(queues[chatID]) > 0 {
			nextInQueue := queues[chatID][0]
			response := fmt.Sprintf("%s, ваше время истекло. Теперь очередь %s.", firstInQueue.Username, nextInQueue.Username)
			msg := tgbotapi.NewMessage(chatID, response)
			msg.ReplyMarkup = getCommandButtons()
			bot.Send(msg)
			startNextUser(bot, chatID) // Start next user's timer
		} else {
			response := fmt.Sprintf("%s, ваше время истекло. Очередь пуста.", firstInQueue.Username)
			msg := tgbotapi.NewMessage(chatID, response)
			msg.ReplyMarkup = getCommandButtons()
			bot.Send(msg)
		}
	})
}

func handleStopTime(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	mu.Lock()
	defer mu.Unlock()

	chatID := message.Chat.ID
	userID := message.From.ID

	if ownerID, exists := countdownOwners[chatID]; !exists || ownerID != userID {
		msg := tgbotapi.NewMessage(chatID, "Только пользователь, начавший отсчёт времени, может его остановить.")
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	delete(activeCountdowns, chatID)
	delete(countdownOwners, chatID)

	var nextInQueueMessage string
	if len(queues[chatID]) > 0 {
		nextInQueue := queues[chatID][0]
		nextInQueueMessage = fmt.Sprintf("Следующий в очереди: %s", nextInQueue.Username)
	} else {
		nextInQueueMessage = "Очередь пуста."
	}

	response := fmt.Sprintf("%s остановил отсчёт времени.\n%s", message.From.FirstName, nextInQueueMessage)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)

	// Start next user's timer
	startNextUser(bot, chatID)
}

func handleQueue(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	mu.Lock()
	chatID := message.Chat.ID
	queue := queues[chatID]
	mu.Unlock()

	if len(queue) == 0 {
		msg := tgbotapi.NewMessage(chatID, "Очередь на данный момент пуста.")
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	response := "Текущая очередь:\n"
	for i, item := range queue {
		response += fmt.Sprintf("%d. %s\n", i+1, item.Username)
	}

	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)
}

func handleRemove(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	args := strings.Fields(message.CommandArguments())
	if len(args) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Пожалуйста, укажите номер в очереди для удаления. Пример: /remove 1")
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	position, err := strconv.Atoi(args[0])
	if err != nil || position < 1 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Неверный номер. Пожалуйста, укажите корректный номер в очереди.")
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	chatID := message.Chat.ID
	if position > len(queues[chatID]) {
		msg := tgbotapi.NewMessage(chatID, "Неверный номер. Пожалуйста, укажите корректный номер в очереди.")
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	removedUser := queues[chatID][position-1]
	queues[chatID] = append(queues[chatID][:position-1], queues[chatID][position:]...)

	response := fmt.Sprintf("Пользователь %s был удален из очереди.", removedUser.Username)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)
}

func handleHelp(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	response := "Доступные команды:\n" +
		"/join - Занять очередь\n" +
		"/stoptime - Остановить отсчёт времени (только пользователь, начавший отсчёт)\n" +
		"/queue - Показать очередь\n" +
		"/remove <номер> - Удалить пользователя из очереди по номеру\n" +
		"/help - Показать доступные команды"

	msg := tgbotapi.NewMessage(message.Chat.ID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)
}

func getCommandButtons() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("/join"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("/stoptime"),
			tgbotapi.NewKeyboardButton("/queue"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("/help"),
		),
	)
}
