package telegram

import (
	"context"
	"fmt"
	"log/slog"

	"strings"
	"strconv"
	"syscall"
	"sync"
	"time"

	"anilibria-bot/internal/anilibria"
	"anilibria-bot/internal/collage"
	"anilibria-bot/internal/config"
	"anilibria-bot/internal/db"
	"anilibria-bot/internal/qbittorrent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api       *tgbotapi.BotAPI
	cfg       *config.Config
	db        *db.Database
	aniClient *anilibria.Client
	qbClient  *qbittorrent.Client
}

func New(cfg *config.Config, database *db.Database, aniClient *anilibria.Client, qbClient *qbittorrent.Client) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	slog.Info("Authorized on account", "username", api.Self.UserName)

	return &Bot{
		api:       api,
		cfg:       cfg,
		db:        database,
		aniClient: aniClient,
		qbClient:  qbClient,
	}, nil
}

func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping Telegram bot...")
			b.api.StopReceivingUpdates()
			return
		case update := <-updates:
			b.handleUpdate(update)
		}
	}
}

func (b *Bot) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return err
}

func (b *Bot) SendNotification(chatID int64, text string, photoURL string) error {
	if photoURL != "" {
		msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(photoURL))
		msg.Caption = text
		msg.ParseMode = tgbotapi.ModeMarkdown
		_, err := b.api.Send(msg)
		return err
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	_, err := b.api.Send(msg)
	return err
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		if !b.cfg.IsAdmin(update.CallbackQuery.From.ID) {
			return
		}
		b.handleCallback(update.CallbackQuery)
		return
	}

	if update.Message != nil {
		// Verify admin
		if !b.cfg.IsAdmin(update.Message.From.ID) {
			slog.Warn("Unauthorized access attempt", "user_id", update.Message.From.ID)
			return
		}

		if update.Message.IsCommand() {
			b.handleCommand(update.Message)
			return
		}

		b.handleMessage(update.Message)
	}
}

func (b *Bot) handleCommand(message *tgbotapi.Message) {
	switch message.Command() {
	case "start":
		msg := tgbotapi.NewMessage(message.Chat.ID, "Привет! Я бот для отслеживания релизов AniLibria.\nВоспользуйтесь меню ниже для управления.")
		
		keyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("📅 Сегодня"),
				tgbotapi.NewKeyboardButton("🗓 Расписание на неделю"),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("🔍 Поиск"),
				tgbotapi.NewKeyboardButton("📋 Мои подписки"),
				tgbotapi.NewKeyboardButton("🧲 Торренты"),
			),
		)
		msg.ReplyMarkup = keyboard
		
		b.api.Send(msg)
	}
}

func (b *Bot) handleMessage(message *tgbotapi.Message) {
	text := message.Text
	switch text {
	case "📅 Сегодня":
		releases, err := b.aniClient.GetScheduleToday()
		if err != nil {
			b.api.Send(tgbotapi.NewMessage(message.Chat.ID, "Не удалось получить расписание или сегодня нет релизов."))
			return
		}
		b.sendSchedule(message.Chat.ID, message.From.ID, "📅 *Выходит сегодня*", releases, 0, 0)

	case "🗓 Расписание на неделю":
		msg := tgbotapi.NewMessage(message.Chat.ID, "Выберите день недели:")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Пн", "day_1"),
				tgbotapi.NewInlineKeyboardButtonData("Вт", "day_2"),
				tgbotapi.NewInlineKeyboardButtonData("Ср", "day_3"),
				tgbotapi.NewInlineKeyboardButtonData("Чт", "day_4"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Пт", "day_5"),
				tgbotapi.NewInlineKeyboardButtonData("Сб", "day_6"),
				tgbotapi.NewInlineKeyboardButtonData("Вс", "day_7"),
			),
		)
		b.api.Send(msg)

	case "🔍 Поиск":
		b.api.Send(tgbotapi.NewMessage(message.Chat.ID, "Напишите название аниме для поиска (любой текст):"))

	case "🧲 Торренты":
		b.sendTorrentsMenu(message.Chat.ID, message.From.ID)

	case "⬇️ Мои загрузки":
		b.sendDownloadsList(message.Chat.ID, 0)

	case "📋 Мои подписки":
		releases, err := b.getSubscribedReleases(message.From.ID)
		if err != nil || len(releases) == 0 {
			b.api.Send(tgbotapi.NewMessage(message.Chat.ID, "У вас пока нет подписок."))
			return
		}
		
		b.sendSchedule(message.Chat.ID, message.From.ID, "📋 *Ваши подписки*", releases, -1, 0)

	default:
		// Perform search
		releases, err := b.aniClient.SearchReleases(text)
		if err != nil {
			b.api.Send(tgbotapi.NewMessage(message.Chat.ID, "Ошибка при поиске."))
			return
		}
		if len(releases) == 0 {
			b.api.Send(tgbotapi.NewMessage(message.Chat.ID, "Ничего не найдено."))
			return
		}

		// Get existing subscriptions
		subs, _ := b.db.GetUserSubscriptions(message.From.ID)
		subMap := make(map[int]bool)
		for _, id := range subs {
			subMap[id] = true
		}

		for i, r := range releases {
			if i >= 3 {
				break
			}
			
			caption := fmt.Sprintf("Найдено: *%s*\nID: %d", r.Name.Main, r.ID)
			var btn tgbotapi.InlineKeyboardMarkup
			if subMap[r.ID] {
				btn = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("❌ Отписаться", fmt.Sprintf("unsub_%d", r.ID)),
						tgbotapi.NewInlineKeyboardButtonData("📥 Скачать", fmt.Sprintf("dl_title_%d", r.ID)),
					),
				)
			} else {
				btn = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🔔 Подписаться", fmt.Sprintf("sub_%d", r.ID)),
						tgbotapi.NewInlineKeyboardButtonData("📥 Скачать", fmt.Sprintf("dl_title_%d", r.ID)),
					),
				)
			}

			if r.Poster.Src != "" {
				photoURL := "https://www.anilibria.top" + r.Poster.Src
				msg := tgbotapi.NewPhoto(message.Chat.ID, tgbotapi.FileURL(photoURL))
				msg.Caption = caption
				msg.ParseMode = tgbotapi.ModeMarkdown
				msg.ReplyMarkup = btn
				b.api.Send(msg)
			} else {
				msg := tgbotapi.NewMessage(message.Chat.ID, caption)
				msg.ParseMode = tgbotapi.ModeMarkdown
				msg.ReplyMarkup = btn
				b.api.Send(msg)
			}
		}
	}
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	data := query.Data
	chatID := query.Message.Chat.ID
	userID := query.From.ID

	if strings.HasPrefix(data, "day_") {
		dayStr := strings.TrimPrefix(data, "day_")
		dayInt, _ := strconv.Atoi(dayStr)
		
		daysMap := map[int]string{1:"Понедельник", 2:"Вторник", 3:"Среда", 4:"Четверг", 5:"Пятница", 6:"Суббота", 7:"Воскресенье"}
		
		releases, err := b.aniClient.GetScheduleWeek()
		if err != nil {
			b.api.Request(tgbotapi.NewCallback(query.ID, "Ошибка получения расписания"))
			return
		}
		
		var dayReleases []anilibria.Release
		for _, r := range releases {
			if r.PublishDay.Value == dayInt {
				dayReleases = append(dayReleases, r)
			}
		}
		
		b.api.Request(tgbotapi.NewCallback(query.ID, ""))
		b.sendSchedule(chatID, userID, fmt.Sprintf("🗓 *Расписание на %s*", daysMap[dayInt]), dayReleases, dayInt, 0)
		return
	}

	if strings.HasPrefix(data, "page_") {
		parts := strings.Split(data, "_")
		if len(parts) == 3 {
			dayInt, _ := strconv.Atoi(parts[1])
			page, _ := strconv.Atoi(parts[2])

			var releases []anilibria.Release
			var err error
			var header string

			if dayInt == 0 {
				releases, err = b.aniClient.GetScheduleToday()
				header = "📅 *Выходит сегодня*"
			} else if dayInt == -1 {
				releases, err = b.getSubscribedReleases(userID)
				header = "📋 *Ваши подписки*"
			} else {
				releases, err = b.aniClient.GetScheduleWeek()
				var dayReleases []anilibria.Release
				for _, r := range releases {
					if r.PublishDay.Value == dayInt {
						dayReleases = append(dayReleases, r)
					}
				}
				releases = dayReleases
				daysMap := map[int]string{1:"Понедельник", 2:"Вторник", 3:"Среда", 4:"Четверг", 5:"Пятница", 6:"Суббота", 7:"Воскресенье"}
				header = fmt.Sprintf("🗓 *Расписание на %s*", daysMap[dayInt])
			}

			if err != nil {
				b.api.Request(tgbotapi.NewCallback(query.ID, "Ошибка"))
				return
			}
			
			b.api.Send(tgbotapi.NewDeleteMessage(chatID, query.Message.MessageID))
			b.sendSchedule(chatID, userID, header, releases, dayInt, page)
		}
		b.api.Request(tgbotapi.NewCallback(query.ID, ""))
		return
	}

	if strings.HasPrefix(data, "dl_list_") {
		if !b.checkTorrentClientAlive(query.ID) {
			return
		}
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "dl_list_"))
		b.sendDownloadsList(chatID, page)
		b.api.Request(tgbotapi.NewCallback(query.ID, ""))
		return
	}

	if strings.HasPrefix(data, "dl_action_") {
		if !b.checkTorrentClientAlive(query.ID) {
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(data, "dl_action_"), "_", 2)
		action := parts[0]
		hash := parts[1]

		switch action {
		case "pause": b.qbClient.PauseTorrent(hash)
		case "resume": b.qbClient.ResumeTorrent(hash)
		case "delete": b.qbClient.DeleteTorrent(hash, true)
		}
		b.sendDownloadsList(chatID, 0)
		b.api.Request(tgbotapi.NewCallback(query.ID, "✅ Действие выполнено"))
		return
	}

	if data == "toggle_autodl" {
		current, _ := b.db.GetAutoDownload(userID)
		b.db.SetAutoDownload(userID, !current)
		
		b.api.Request(tgbotapi.NewCallback(query.ID, "Настройки сохранены"))
		
		// Edit message to update the button
		b.api.Send(tgbotapi.NewDeleteMessage(chatID, query.Message.MessageID))
		b.sendTorrentsMenu(chatID, userID)
		return
	}

	if data == "dl_info" {
		var stat syscall.Statfs_t
		err := syscall.Statfs(".", &stat)
		if err != nil {
			b.api.Request(tgbotapi.NewCallback(query.ID, "Не удалось получить инфо о месте"))
			return
		}
		
		// Space in GB
		freeSpace := float64(stat.Bavail*uint64(stat.Bsize)) / (1024 * 1024 * 1024)
		totalSpace := float64(stat.Blocks*uint64(stat.Bsize)) / (1024 * 1024 * 1024)
		usedSpace := totalSpace - freeSpace

		text := fmt.Sprintf("💾 *Информация о диске*\nСвободно: %.2f GB\nЗанято: %.2f GB\nВсего: %.2f GB", freeSpace, usedSpace, totalSpace)
		
		b.api.Request(tgbotapi.NewCallback(query.ID, ""))
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		b.api.Send(msg)
		return
	}

	if data == "dl_subs" {
		subs, err := b.db.GetUserSubscriptions(userID)
		if err != nil || len(subs) == 0 {
			b.api.Request(tgbotapi.NewCallback(query.ID, "У вас нет подписок."))
			return
		}

		b.api.Request(tgbotapi.NewCallback(query.ID, ""))
		
		msg := tgbotapi.NewMessage(chatID, "Выберите тайтл для скачивания (сначала нажмите Найти или выберите из списка):")
		var rows [][]tgbotapi.InlineKeyboardButton
		for i, id := range subs {
			if i >= 10 {
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("И другие...", "none"),
				))
				break
			}
			
			titleName := fmt.Sprintf("Тайтл ID %d", id)
			torrents, err := b.aniClient.GetTorrents(id)
			if err == nil && len(torrents) > 0 && torrents[0].Release != nil {
				titleName = torrents[0].Release.Name.Main
			}

			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(titleName, fmt.Sprintf("dl_title_%d", id)),
			))
		}
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		b.api.Send(msg)
		return
	}

	if data == "dl_search" {
		b.api.Request(tgbotapi.NewCallback(query.ID, "Воспользуйтесь кнопкой '🔍 Поиск' в главном меню"))
		return
	}

	if strings.HasPrefix(data, "dl_title_") {
		if !b.checkTorrentClientAlive(query.ID) {
			return
		}
		idStr := strings.TrimPrefix(data, "dl_title_")
		titleID, _ := strconv.Atoi(idStr)

		torrents, err := b.aniClient.GetTorrents(titleID)
		if err != nil || len(torrents) == 0 {
			b.api.Request(tgbotapi.NewCallback(query.ID, "Торренты не найдены"))
			return
		}

		bestTorrent := torrents[0]
		for _, t := range torrents {
			if t.Quality.Value == "1080p" {
				bestTorrent = t
				break
			}
		}

		titleName := "Тайтл"
		if bestTorrent.Release != nil {
			titleName = bestTorrent.Release.Name.Main
		}

		b.api.Request(tgbotapi.NewCallback(query.ID, "⏳ Подгрузка списка файлов..."))
		
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("⏳ *%s*\nИдёт получение метаданных торрента, пожалуйста, подождите (до 15 секунд)...", titleName))
		msg.ParseMode = tgbotapi.ModeMarkdown
		sentMsg, err := b.api.Send(msg)
		if err != nil {
			return
		}

		go func() {
			hash := bestTorrent.Hash
			
			// Try to get files first (maybe it's already in qbittorrent)
			files, err := b.qbClient.GetFiles(hash)
			if err != nil || len(files) == 0 {
				err = b.qbClient.AddTorrentPaused(bestTorrent.Magnet, "", "Anime")
				if err != nil {
					b.qbClient.Login()
					b.qbClient.AddTorrentPaused(bestTorrent.Magnet, "", "Anime")
				}
				
				// Poll for files
				for i := 0; i < 20; i++ {
					time.Sleep(2 * time.Second)
					files, err = b.qbClient.GetFiles(hash)
					if err == nil && len(files) > 0 {
						break
					}
				}
			}

			if err != nil || len(files) == 0 {
				edit := tgbotapi.NewEditMessageText(chatID, sentMsg.MessageID, "❌ Не удалось получить список файлов торрента. Попробуйте скачать всё.")
				b.api.Send(edit)
				return
			}

			// Filter video files
			var videoFiles []qbittorrent.TorrentFile
			var allIndices []int
			for _, f := range files {
				allIndices = append(allIndices, f.Index)
				lower := strings.ToLower(f.Name)
				if strings.HasSuffix(lower, ".mkv") || strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".avi") {
					videoFiles = append(videoFiles, f)
				}
			}

			if len(videoFiles) == 0 {
				videoFiles = files
			}

			// Set all to priority 0 so we can selectively download
			b.qbClient.SetFilePriorities(hash, allIndices, 0)

			allSizeMB := bestTorrent.Size / (1024 * 1024)
			var textBuilder strings.Builder
			textBuilder.WriteString(fmt.Sprintf("📥 *%s*\n\nРазмер всего: ~%d MB\nФайлы:\n", titleName, allSizeMB))

			var rows [][]tgbotapi.InlineKeyboardButton
			var currentRow []tgbotapi.InlineKeyboardButton

			for i, f := range videoFiles {
				sizeMB := f.Size / (1024 * 1024)
				
				displayNum := i + 1
				textBuilder.WriteString(fmt.Sprintf("%d — %d MB\n", displayNum, sizeMB))

				btn := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d", displayNum), fmt.Sprintf("dl_idx_%s_%d", hash, f.Index))
				currentRow = append(currentRow, btn)

				if len(currentRow) == 5 {
					rows = append(rows, currentRow)
					currentRow = nil
				}
				
				// Max 50 files shown to avoid telegram limits
				if i >= 49 {
					textBuilder.WriteString("...и другие файлы.\n")
					break
				}
			}

			if len(currentRow) > 0 {
				rows = append(rows, currentRow)
			}

			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Скачать всё", fmt.Sprintf("dl_all_%s", hash)),
			))

			text := textBuilder.String()
			if len(text) > 4000 {
				text = text[:4000] + "..."
			}

			edit := tgbotapi.NewEditMessageText(chatID, sentMsg.MessageID, text)
			edit.ParseMode = tgbotapi.ModeMarkdown
			markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
			edit.ReplyMarkup = &markup
			b.api.Send(edit)
		}()
		return
	}

	if strings.HasPrefix(data, "dl_idx_") {
		if !b.checkTorrentClientAlive(query.ID) {
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(data, "dl_idx_"), "_", 2)
		if len(parts) == 2 {
			hash := parts[0]
			idx, _ := strconv.Atoi(parts[1])

			b.qbClient.SetFilePriorities(hash, []int{idx}, 1)
			b.qbClient.ResumeTorrent(hash)

			b.api.Request(tgbotapi.NewCallback(query.ID, "✅ Добавлено в загрузки!"))
			return
		}
	}

	if strings.HasPrefix(data, "dl_all_") {
		if !b.checkTorrentClientAlive(query.ID) {
			return
		}
		hash := strings.TrimPrefix(data, "dl_all_")
		
		// If it's a hash, we already fetched files
		if len(hash) > 10 {
			files, err := b.qbClient.GetFiles(hash)
			if err == nil {
				var allIndices []int
				for _, f := range files {
					allIndices = append(allIndices, f.Index)
				}
				b.qbClient.SetFilePriorities(hash, allIndices, 1)
				b.qbClient.ResumeTorrent(hash)
				b.api.Request(tgbotapi.NewCallback(query.ID, "✅ Загрузка всего тайтла начата!"))
				return
			}
		}

		b.api.Request(tgbotapi.NewCallback(query.ID, "Старый формат кнопки не поддерживается"))
		return
	}

	if strings.HasPrefix(data, "sub_") {
		idStr := strings.TrimPrefix(data, "sub_")
		titleID, _ := strconv.Atoi(idStr)

		err := b.db.Subscribe(userID, titleID)
		var text string
		if err != nil {
			text = "Ошибка при подписке."
		} else {
			text = "✅ Успешно подписано!"

			if query.Message.ReplyMarkup != nil {
				keyboard := query.Message.ReplyMarkup.InlineKeyboard
				for r := range keyboard {
					for c := range keyboard[r] {
						btn := &keyboard[r][c]
						if btn.CallbackData != nil && *btn.CallbackData == data {
							newText := "❌ Отписаться"
							if strings.HasPrefix(btn.Text, "🔔 ") {
								newText = "❌ " + strings.TrimPrefix(btn.Text, "🔔 ")
							}
							btn.Text = newText
							newData := fmt.Sprintf("unsub_%d", titleID)
							btn.CallbackData = &newData
						}
					}
				}
				edit := tgbotapi.NewEditMessageReplyMarkup(chatID, query.Message.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard})
				b.api.Send(edit)
			}
		}
		b.api.Request(tgbotapi.NewCallback(query.ID, text))

	} else if strings.HasPrefix(data, "unsub_") {
		idStr := strings.TrimPrefix(data, "unsub_")
		titleID, _ := strconv.Atoi(idStr)

		err := b.db.Unsubscribe(userID, titleID)
		var text string
		if err != nil {
			text = "Ошибка при отписке."
		} else {
			text = "✅ Успешно отписано!"

			if query.Message.ReplyMarkup != nil {
				keyboard := query.Message.ReplyMarkup.InlineKeyboard
				for r := range keyboard {
					for c := range keyboard[r] {
						btn := &keyboard[r][c]
						if btn.CallbackData != nil && *btn.CallbackData == data {
							newText := "🔔 Подписаться"
							if strings.HasPrefix(btn.Text, "❌ ") {
								newText = "🔔 " + strings.TrimPrefix(btn.Text, "❌ ")
							}
							btn.Text = newText
							newData := fmt.Sprintf("sub_%d", titleID)
							btn.CallbackData = &newData
						}
					}
				}
				edit := tgbotapi.NewEditMessageReplyMarkup(chatID, query.Message.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard})
				b.api.Send(edit)
			}
		}
		b.api.Request(tgbotapi.NewCallback(query.ID, text))
	}
}

func (b *Bot) getSubscribedReleases(userID int64) ([]anilibria.Release, error) {
	subs, err := b.db.GetUserSubscriptions(userID)
	if err != nil {
		return nil, err
	}

	var releases []anilibria.Release
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, id := range subs {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			torrents, err := b.aniClient.GetTorrents(id)
			if err == nil && len(torrents) > 0 && torrents[0].Release != nil {
				mu.Lock()
				releases = append(releases, *torrents[0].Release)
				mu.Unlock()
			}
		}(id)
	}

	wg.Wait()
	return releases, nil
}

func (b *Bot) sendTorrentsMenu(chatID int64, userID int64) {
	autoDl, _ := b.db.GetAutoDownload(userID)
	autoDlText := "⚙️ Авто-скачивание: Выкл"
	if autoDl {
		autoDlText = "⚙️ Авто-скачивание: Вкл"
	}

	msg := tgbotapi.NewMessage(chatID, "🧲 *Управление торрентами*\nЗдесь вы можете настроить скачивание релизов.")
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬇️ Мои загрузки", "dl_list_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(autoDlText, "toggle_autodl"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📥 Скачать из подписок", "dl_subs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔍 Найти для скачивания", "dl_search"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💾 Инфо о месте", "dl_info"),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) sendSchedule(chatID int64, userID int64, header string, releases []anilibria.Release, dayInt int, page int) {
	if len(releases) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, header+"\n\nНет релизов."))
		return
	}

	maxPerPage := 9
	if len(releases) <= 10 {
		maxPerPage = 10
	}

	startIdx := page * maxPerPage
	endIdx := startIdx + maxPerPage
	if endIdx > len(releases) {
		endIdx = len(releases)
	}
	
	if startIdx >= len(releases) {
		return // Should not happen
	}

	pageReleases := releases[startIdx:endIdx]

	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("%s (%d тайтлов):\n\n", header, len(releases)))

	var rows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton
	var posterURLs []string

	subs, _ := b.db.GetUserSubscriptions(userID)
	subMap := make(map[int]bool)
	for _, id := range subs {
		subMap[id] = true
	}

	for i, r := range pageReleases {
		displayNum := startIdx + i + 1
		textBuilder.WriteString(fmt.Sprintf("%d. %s\n", displayNum, r.Name.Main))
		posterURLs = append(posterURLs, r.Poster.Src)

		var btn tgbotapi.InlineKeyboardButton
		if subMap[r.ID] {
			btn = tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("❌ %d", displayNum), fmt.Sprintf("unsub_%d", r.ID))
		} else {
			btn = tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("🔔 %d", displayNum), fmt.Sprintf("sub_%d", r.ID))
		}
		currentRow = append(currentRow, btn)

		if len(currentRow) == 5 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	// Pagination buttons
	var navRow []tgbotapi.InlineKeyboardButton
	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("page_%d_%d", dayInt, page-1)))
	}
	if endIdx < len(releases) {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("➡️ Далее", fmt.Sprintf("page_%d_%d", dayInt, page+1)))
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	if dayInt == -1 {
		textBuilder.WriteString("\nВыберите номер для отписки:")
	} else {
		textBuilder.WriteString("\nВыберите номер для подписки:")
	}

	collageBytes, err := collage.GenerateScheduleCollage(posterURLs, startIdx)

	var msg tgbotapi.Chattable
	if err == nil && len(collageBytes) > 0 {
		photoMsg := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{Name: "schedule.png", Bytes: collageBytes})
		photoMsg.Caption = textBuilder.String()
		photoMsg.ParseMode = tgbotapi.ModeMarkdown
		photoMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg = photoMsg
	} else {
		textMsg := tgbotapi.NewMessage(chatID, textBuilder.String())
		textMsg.ParseMode = tgbotapi.ModeMarkdown
		textMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg = textMsg
	}

	b.api.Send(msg)
}

func (b *Bot) formatSize(bytes int64) string {
	mb := float64(bytes) / (1024 * 1024)
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.0f MB", mb)
}

func (b *Bot) formatDuration(seconds int64) string {
	if seconds >= 8640000 { // Max ETA is often 8640000 (100 days)
		return "∞"
	}
	d := time.Duration(seconds) * time.Second
	return d.String()
}

func (b *Bot) sendDownloadsList(chatID int64, page int) {
	torrents, err := b.qbClient.GetTorrentsList("Anime")
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "Ошибка получения списка загрузок."))
		return
	}

	if len(torrents) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "У вас нет активных или завершённых загрузок от бота."))
		return
	}

	itemsPerPage := 5
	startIdx := page * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(torrents) {
		endIdx = len(torrents)
	}

	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("⬇️ *Ваши загрузки* (%d всего):\n\n", len(torrents)))

	var keyboard [][]tgbotapi.InlineKeyboardButton

	for i := startIdx; i < endIdx; i++ {
		t := torrents[i]
		displayNum := i + 1

		status := "⏳"
		if t.State == "downloading" || t.State == "metaDL" || t.State == "stalledDL" {
			status = "⬇️"
		} else if t.State == "uploading" || t.State == "stalledUP" {
			status = "✅"
		} else if t.State == "pausedDL" || t.State == "pausedUP" {
			status = "⏸"
		}

		textBuilder.WriteString(fmt.Sprintf("%d. %s %s\n", displayNum, status, t.Name))
		textBuilder.WriteString(fmt.Sprintf("Прогресс: %.1f%%\n", t.Progress*100))
		textBuilder.WriteString(fmt.Sprintf("Скачано: %s / %s\n", b.formatSize(t.Downloaded), b.formatSize(t.Size)))
		
		if t.State == "downloading" || t.State == "metaDL" {
			textBuilder.WriteString(fmt.Sprintf("Осталось: %s\n", b.formatDuration(t.Eta)))
		}
		textBuilder.WriteString("\n")

		// Buttons for this torrent
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("⏸ %d", displayNum), fmt.Sprintf("dl_action_pause_%s", t.Hash)),
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("▶️ %d", displayNum), fmt.Sprintf("dl_action_resume_%s", t.Hash)),
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("❌ %d", displayNum), fmt.Sprintf("dl_action_delete_%s", t.Hash)),
		)
		keyboard = append(keyboard, row)
	}

	// Pagination
	var navRow []tgbotapi.InlineKeyboardButton
	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("dl_list_%d", page-1)))
	}
	if endIdx < len(torrents) {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("dl_list_%d", page+1)))
	}
	if len(navRow) > 0 {
		keyboard = append(keyboard, navRow)
	}

	msg := tgbotapi.NewMessage(chatID, textBuilder.String())
	msg.ParseMode = "Markdown"
	if len(keyboard) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	}

	b.api.Send(msg)
}

func (b *Bot) checkTorrentClientAlive(queryID string) bool {
	if err := b.qbClient.CheckAlive(); err != nil {
		b.api.Request(tgbotapi.NewCallback(queryID, "❌ Торрент-клиент недоступен"))
		return false
	}
	return true
}
