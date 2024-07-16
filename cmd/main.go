package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type QueueItem struct {
	Username string
	UserID   int
}

var queues = make(map[int64][]QueueItem)
var activeCountdowns = make(map[int64]time.Time)
var countdownOwners = make(map[int64]int)
var countdownCancelFuncs = make(map[int64]context.CancelFunc)
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
			case "help":
				handleHelp(bot, update.Message)
			default:
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Я не знаю такую команду, вы можете воспользоваться /help чтобы посмотреть список поддерживаемых команд.")
				bot.Send(msg)
			}
		}
	}
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
		Username: message.From.FirstName + " (@" + message.From.UserName + ")",
		UserID:   userID,
	})

	response := fmt.Sprintf("%s занял очередь.", message.From.FirstName)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)

	go handleQueue(bot, message) // Automatically show the queue after joining

	// Start the countdown automatically if this user is the first in the queue
	if len(queues[chatID]) == 1 {
		startCountdown(bot, chatID, queues[chatID][0])
	}
}

func startCountdown(bot *tgbotapi.BotAPI, chatID int64, user QueueItem) {
	mu.Lock()
	if _, exists := activeCountdowns[chatID]; exists {
		mu.Unlock()
		return
	}

	countdownOwners[chatID] = user.UserID
	activeCountdowns[chatID] = time.Now().Add(10 * time.Minute)

	// Create a context with cancel function to manage the countdown
	ctx, cancel := context.WithCancel(context.Background())
	countdownCancelFuncs[chatID] = cancel
	mu.Unlock()

	startTime := time.Now()
	endTime := startTime.Add(10 * time.Minute)

	var nextInQueueMessage string
	mu.Lock()
	if len(queues[chatID]) > 1 {
		nextInQueue := queues[chatID][1]
		nextInQueueMessage = fmt.Sprintf("Следующий в очереди: %s", nextInQueue.Username)
	} else {
		nextInQueueMessage = "Очередь пуста."
	}
	mu.Unlock()

	response := fmt.Sprintf("%s начал отсчёт времени.\nПромежуток: %s - %s\n%s", user.Username, startTime.Format("15:04:05"), endTime.Format("15:04:05"), nextInQueueMessage)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)

	go func() {
		select {
		case <-ctx.Done():
			// Countdown was stopped early
			return
		case <-time.After(10 * time.Minute):
			mu.Lock()
			defer mu.Unlock()

			delete(activeCountdowns, chatID)
			delete(countdownOwners, chatID)
			delete(countdownCancelFuncs, chatID)

			if len(queues[chatID]) > 0 {
				nextInQueue := queues[chatID][0]
				queues[chatID] = queues[chatID][1:]
				response := fmt.Sprintf("%s, ваше время истекло. Теперь очередь %s.", user.Username, nextInQueue.Username)
				msg := tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = getCommandButtons()
				bot.Send(msg)

				// Start the countdown for the next user in the queue
				go startCountdown(bot, chatID, nextInQueue)
			} else {
				response := fmt.Sprintf("%s, ваше время истекло. Очередь пуста.", user.Username)
				msg := tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = getCommandButtons()
				bot.Send(msg)
			}
		}
	}()
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

	// Cancel the countdown context
	if cancel, exists := countdownCancelFuncs[chatID]; exists {
		cancel()
		delete(countdownCancelFuncs, chatID)
	}

	delete(activeCountdowns, chatID)
	delete(countdownOwners, chatID)

	var nextInQueueMessage string
	if len(queues[chatID]) > 0 {
		nextInQueue := queues[chatID][0]
		nextInQueueMessage = fmt.Sprintf("Следующий в очереди: %s", nextInQueue.Username)
		go startCountdown(bot, chatID, nextInQueue)
	} else {
		nextInQueueMessage = "Очередь пуста."
	}

	response := fmt.Sprintf("%s остановил отсчёт времени.\n%s", message.From.FirstName, nextInQueueMessage)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)
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

func handleHelp(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	response := "Доступные команды:\n" +
		"/join - Занять очередь\n" +
		"/stoptime - Остановить отсчёт времени (только пользователь, начавший отсчёт)\n" +
		"/queue - Показать очередь\n" +
		"/help - Показать доступные команды"

	msg := tgbotapi.NewMessage(message.Chat.ID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)
}

func getCommandButtons() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Занять очередь /join"),
			tgbotapi.NewKeyboardButton("Остановить отсчёт времени /stoptime"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Показать очередь /queue"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Помощь /help"),
		),
	)
}
