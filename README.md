
<div align="center">
  <img src="https://i.pinimg.com/736x/13/e6/39/13e639a72d640f6ccf42d748a3b946f8.jpg" width="200" height="200" alt="MaoMao Logo" style="border-radius: 50%; object-fit: cover;" />
  
  <h1 align="center">🌟 AniLibria Tracker Bot 🌟</h1>
  <p align="center"><b>Telegram-бот для отслеживания выхода новых серий на AniLibria!</b></p>
  
  <p align="center">
    <a href="https://golang.org/">
      <img src="https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go" />
    </a>
    <a href="https://sqlite.org/">
      <img src="https://img.shields.io/badge/SQLite-07405E?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite" />
    </a>
    <a href="https://www.qbittorrent.org/">
      <img src="https://img.shields.io/badge/qBittorrent-2D547C?style=for-the-badge&logo=qbittorrent&logoColor=white" alt="qBittorrent" />
    </a>
    <a href="https://core.telegram.org/bots">
      <img src="https://img.shields.io/badge/Telegram-2CA5E0?style=for-the-badge&logo=telegram&logoColor=white" alt="Telegram" />
    </a>
  </p>
</div>

##  О проекте 

Написан на Go, автоматически проверяет релизы, отправляет уведомления с красивыми коллажами и умеет загружать торренты напрямую в локальный qBittorrent. Больше не нужно вручную проверять сайты — бот сделает всё за вас!

### 🛠 Стек технологий

```csharp
Language: Golang
Database: SQLite3
Bot API: go-telegram-bot-api
Graphics: fogleman/gg (Image collages)
Torrent Client: qBittorrent Web API
Deployment: Systemd daemon
Primary Goal: Never miss an episode again!
```


## 🚀 Главные фичи

* **📅 Удобное расписание** — Просмотр релизов на сегодня или на любой день недели с поддержкой стильной пагинации.
* **🖼️ Авто-коллажи** — Бот "на лету" скачивает обложки аниме, кеширует их локально и генерирует красивую сетку-коллаж с нумерацией, чтобы вы не запутались в тайтлах.
* **🔔 Мгновенные подписки** — Подписывайтесь на любимые онгоинги в один клик прямо из расписания или через встроенный поиск.
* **⚡ Авто-скачивание (qBittorrent)** — Глубокая интеграция с локальным торрент-клиентом. Можно настроить авто-скачивание новых серий.
* **📥 Умная загрузка серий** — Бот умеет получать точный список файлов и их размер из метаданных торрента. Качайте весь тайтл или выбирайте конкретную серию (бот автоматически отключит лишние файлы в загрузке).
* **💾 Мониторинг места** — Прямо в Telegram можно проверить, сколько свободного и занятого места осталось на вашем сервере.
* **🔒 Приватность** — Бот является персональным и реагирует только на владельца (ID задается в конфигурации).


## ⚙️ Установка и запуск

### 1. Клонирование и сборка
```bash
git clone https://github.com/anil1te/AnilibriaTrackerBot.git
cd AnilibriaTrackerBot
go build -o AnilibriaTrackerBot ./cmd
```

### 2. Конфигурация
Создайте файл `.env` в корне проекта со своими данными:
```env
TELEGRAM_TOKEN=YOUR_BOT_TOKEN
ADMIN_IDS=123456789
DB_PATH=anilibria.db
QBITTORRENT_URL=http://localhost:8080
QBITTORRENT_USERNAME=admin
QBITTORRENT_PASSWORD=adminadmin
```

> **Как включить веб-интерфейс в qBittorrent?**  
> 1. Откройте клиент **qBittorrent**.  
> 2. Перейдите в меню **Инструменты** ➔ **Настройки** ➔ раздел **Веб-интерфейс** (Tools ➔ Options ➔ Web UI).  
> 3. Поставьте галочку **«Веб-пользовательский интерфейс (Удаленное управление)»**.  
> 4. Задайте порт (по умолчанию `8080`).  
> 5. В блоке «Аутентификация» установите логин и пароль. Именно их нужно указать в `.env`.

### 3. Запуск как Systemd-сервис
(Рекомендуется для работы в фоне 24/7)
```bash
sudo cp AnilibriaTrackerBot.service /etc/systemd/system/
sudo systemctl enable --now AnilibriaTrackerBot
```
