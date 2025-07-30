package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Config struct {
	Integrations []string `json:"integrations"`
	PostgresURL  string   `json:"postgres_url,omitempty"`
	FolderPath   string   `json:"folder_path,omitempty"`
	Frequency    string   `json:"frequency"`
	Storage      string   `json:"storage"`
	LocalPath    string   `json:"local_path,omitempty"`
	BotToken     string   `json:"bot_token,omitempty"`
	ChannelID    int64    `json:"channel_id,omitempty"`
}

func main() {
	backupFlag := flag.Bool("backup", false, "Perform the backup")
	flag.Parse()

	if *backupFlag {
		performBackup()
	} else {
		configureBackup()
	}
}

func configureBackup() {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Please select the integrations to back up:")
	fmt.Println("1. PostgreSQL")
	fmt.Println("2. Storage Folder")
	fmt.Println("3. Both")
	fmt.Print("Enter the number of your choice (1-3): ")
	scanner.Scan()
	choice := scanner.Text()

	var integrations []string
	switch choice {
	case "1":
		integrations = []string{"postgresql"}
	case "2":
		integrations = []string{"folder"}
	case "3":
		integrations = []string{"postgresql", "folder"}
	default:
		fmt.Println("Invalid choice. Exiting.")
		return
	}

	var postgresURL, folderPath string
	if contains(integrations, "postgresql") {
		fmt.Print("Enter PostgreSQL database URL (e.g., postgres://user:pass@host:port/dbname): ")
		scanner.Scan()
		postgresURL = scanner.Text()
	}
	if contains(integrations, "folder") {
		fmt.Print("Enter the storage folder path: ")
		scanner.Scan()
		folderPath = scanner.Text()
	}

	fmt.Println("Select backup frequency:")
	fmt.Println("1. Daily")
	fmt.Println("2. Weekly")
	fmt.Println("3. Monthly")
	fmt.Println("4. Custom (enter cron expression)")
	fmt.Print("Enter your choice (1-4): ")
	scanner.Scan()
	freqChoice := scanner.Text()

	var frequency string
	switch freqChoice {
	case "1":
		frequency = "0 0 * * *" // Daily at midnight
	case "2":
		frequency = "0 0 * * 0" // Weekly on Sunday at midnight
	case "3":
		frequency = "0 0 1 * *" // Monthly on the 1st at midnight
	case "4":
		fmt.Print("Enter custom cron expression (e.g., '0 0 * * *'): ")
		scanner.Scan()
		frequency = scanner.Text()
	default:
		fmt.Println("Invalid choice. Using daily as default.")
		frequency = "0 0 * * *"
	}

	fmt.Println("Where do you want to store the backups?")
	fmt.Println("1. Local Drive")
	fmt.Println("2. Telegram Bot Channel")
	fmt.Print("Enter your choice (1-2): ")
	scanner.Scan()
	storageChoice := scanner.Text()

	var storage, localPath, botToken string
	var channelID int64

	switch storageChoice {
	case "1":
		storage = "local"
		fmt.Print("Enter the local path to store backups: ")
		scanner.Scan()
		localPath = scanner.Text()
	case "2":
		storage = "telegram"
		fmt.Print("Enter Telegram Bot Token: ")
		scanner.Scan()
		botToken = scanner.Text()
		fmt.Print("Enter Telegram Channel ID: ")
		scanner.Scan()
		channelIDStr := scanner.Text()
		var err error
		channelID, err = strconv.ParseInt(channelIDStr, 10, 64)

		if err != nil {
			fmt.Println("Invalid channel ID. Please enter a valid integer.")
			os.Exit(1)
		}

	default:
		fmt.Println("Invalid choice. Exiting.")
		return
	}

	config := Config{
		Integrations: integrations,
		PostgresURL:  postgresURL,
		FolderPath:   folderPath,
		Frequency:    frequency,
		Storage:      storage,
		LocalPath:    localPath,
		BotToken:     botToken,
		ChannelID:    channelID,
	}

	if err := saveConfig(config); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error getting executable path: %v\n", err)
		return
	}
	cronJob := fmt.Sprintf("%s %s --backup", frequency, exePath)
	cmd := exec.Command("sh", "-c", fmt.Sprintf("echo '%s' | crontab -", cronJob))
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error setting up cron job: %v\n", err)
		return
	}

	fmt.Println("Backup configured successfully.")
}

func performBackup() {
	config, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	var backupFiles []string
	if contains(config.Integrations, "postgresql") {
		filename, err := backupPostgres(config.PostgresURL)
		if err != nil {
			fmt.Printf("Error backing up PostgreSQL: %v\n", err)
			return
		}
		backupFiles = append(backupFiles, filename)
	}
	if contains(config.Integrations, "folder") {
		filename, err := backupFolder(config.FolderPath)
		if err != nil {
			fmt.Printf("Error backing up folder: %v\n", err)
			return
		}
		backupFiles = append(backupFiles, filename)
	}

	for _, filename := range backupFiles {
		if config.Storage == "local" {
			if err := storeLocal(filename, config.LocalPath); err != nil {
				fmt.Printf("Error storing backup locally: %v\n", err)
			}
		} else if config.Storage == "telegram" {
			if err := storeTelegram(filename, config.BotToken, config.ChannelID); err != nil {
				fmt.Printf("Error uploading backup to Telegram: %v\n", err)
			}
		}
		os.Remove(filename) // Clean up temporary file
	}
}

func getConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".backup_app", "config.json")
}

func saveConfig(config Config) error {
	path := getConfigPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	return encoder.Encode(config)
}

func loadConfig() (Config, error) {
	path := getConfigPath()
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	var config Config
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	return config, err
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func backupPostgres(url string) (string, error) {
	// Simplified: assumes URL parsing or environment variables are set externally
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("postgres_backup_%s.sql", timestamp)
	cmd := exec.Command("pg_dump", url, "-f", filename)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return filename, nil
}

func backupFolder(path string) (string, error) {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("folder_backup_%s.tar.gz", timestamp)
	cmd := exec.Command("tar", "-czf", filename, path)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return filename, nil
}

func storeLocal(filename, localPath string) error {
	dest := filepath.Join(localPath, filepath.Base(filename))
	return os.Rename(filename, dest)
}

func storeTelegram(filename, botToken string, channelID int64) error {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return err
	}
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	doc := tgbotapi.NewDocument(channelID, tgbotapi.FileReader{
		Name:   filepath.Base(filename),
		Reader: file,
	})
	_, err = bot.Send(doc)
	return err
}
