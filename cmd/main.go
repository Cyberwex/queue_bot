package main

import (
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
			case "starttime":
				handleStartTime(bot, update.Message)
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
}

func handleStartTime(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	mu.Lock()
	chatID := message.Chat.ID

	if endTime, exists := activeCountdowns[chatID]; exists {
		mu.Unlock()
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Невозможно начать новый отсчёт времени, пока не завершился текущий. Вы можете начать новый отсчёт в %s.", endTime.Format("15:04:05")))
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	if len(queues[chatID]) == 0 {
		mu.Unlock()
		msg := tgbotapi.NewMessage(chatID, "Очередь пуста.")
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	firstInQueue := queues[chatID][0]
	if firstInQueue.UserID != message.From.ID {
		mu.Unlock()
		msg := tgbotapi.NewMessage(chatID, "Только первый в очереди может начать отсчёт времени.")
		msg.ReplyMarkup = getCommandButtons()
		bot.Send(msg)
		return
	}

	queues[chatID] = queues[chatID][1:]
	countdownOwners[chatID] = firstInQueue.UserID
	activeCountdowns[chatID] = time.Now().Add(10 * time.Minute)

	startTime := time.Now()
	endTime := startTime.Add(10 * time.Minute)

	var nextInQueueMessage string
	if len(queues[chatID]) > 0 {
		nextInQueue := queues[chatID][0]
		nextInQueueMessage = fmt.Sprintf("Следующий в очереди: %s", nextInQueue.Username)
	} else {
		nextInQueueMessage = "Очередь пуста."
	}

	response := fmt.Sprintf("%s начал отсчёт времени.\nПромежуток: %s - %s\n%s", firstInQueue.Username, startTime.Format("15:04:05"), endTime.Format("15:04:05"), nextInQueueMessage)
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ReplyMarkup = getCommandButtons()
	bot.Send(msg)

	mu.Unlock()

	time.AfterFunc(10*time.Minute, func() {
		mu.Lock()
		defer mu.Unlock()

		delete(activeCountdowns, chatID)
		delete(countdownOwners, chatID)

		if len(queues[chatID]) > 0 {
			nextInQueue := queues[chatID][0]
			response := fmt.Sprintf("%s, ваше время истекло. Теперь очередь %s.", firstInQueue.Username, nextInQueue.Username)
			msg := tgbotapi.NewMessage(chatID, response)
			msg.ReplyMarkup = getCommandButtons()
			bot.Send(msg)
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
		"/starttime - Начать отсчёт времени (только первый в очереди)\n" +
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
			tgbotapi.NewKeyboardButton("/join"),
			tgbotapi.NewKeyboardButton("/starttime"),
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
