package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"github.com/joho/godotenv"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)


var (
	EXCLUDED_NUMBERS map[string]bool
	LOG_DIR          = "logs"
	MESSAGES_DIR     = "messages"
	WHISPER_PROMPT   = `Transcreva com precisão, preservando enunciados conforme falados. Corrija erros ortográficos comuns sem alterar a intenção original. Use pontuação e capitalização de forma natural para facilitar a leitura. Foda-se. Amorzinho.`
)

func loadExcludedNumbers(filePath string) map[string]bool {
	excluded := make(map[string]bool)
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Warning: %s not found. Using default exclusions.\n", filePath)
		return excluded
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			excluded[line] = true
		}
	}
	return excluded
}

func setupDirectories() {
	dirs := []string{LOG_DIR, MESSAGES_DIR}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
	// (Optional) Clean up LOG_DIR here if desired.
}

func main() {
	// Load environment variables from .env
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, continuing with environment variables")
	}

	setupDirectories()
    log = waLog.Logger
	EXCLUDED_NUMBERS = loadExcludedNumbers("exclude.txt")
	log.Printf("Loaded excluded numbers: %v", EXCLUDED_NUMBERS)

	// Initialize WhatsMeow client with sqlite storage (adjust DSN as needed)
	// dbLog := log.New(os.Stdout, "DB: ", log.LstdFlags)
	// waLog := log.New(os.Stdout, "WhatsApp: ", log.LstdFlags)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    logger.Info("Whatsmeow message: ")
    logger.Debug("DB message: ")
    logger.Warn("Warning message")
    logger.Error("Error message")
	// waLog.Logger=logger.Info
	container, err := sqlstore.New("sqlite3", "file:db.sqlite3?_foreign_keys=on", log)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		log.Fatalf("Failed to get device: %v", err)
	}
	client := whatsmeow.NewClient(deviceStore, waLog.Logger)

	// Register event handler for incoming messages
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			// Handle each message in its own goroutine
			go handleMessage(client, v)
		}
	})

	// Connect to WhatsApp
	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	log.Println("Connected to WhatsApp")

	// Wait for termination signal (SIGINT/SIGTERM)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down...")
	client.Disconnect()
}

// handleMessage inspects incoming messages and routes audio messages for transcription.
func handleMessage(client *whatsmeow.Client, evt *events.Message) {
	// Skip group messages
	if evt.Info.Chat.is.group {
		log.Println("Message is from a group, ignoring...")
		return
	}

	// Check if sender is excluded
	sender := evt.Info.Sender.User
	if EXCLUDED_NUMBERS[sender] {
		log.Printf("Sender %s is excluded. Skipping transcription.", sender)
		return
	}

	// Check if message contains an audio message.
	// (Adjust the field access based on WhatsMeow’s message structure.)
	if audioMsg := evt.Message.GetAudioMessage(); audioMsg != nil {
		log.Println("Audio message detected, processing transcription...")
		if err := processAudioMessage(client, evt); err != nil {
			log.Printf("Error processing audio message: %v", err)
		}
	} else {
		log.Println("Received non-audio message, ignoring...")
	}
}

// processAudioMessage downloads the audio, calls the transcription API, and sends a reply.
func processAudioMessage(client *whatsmeow.Client, evt *events.Message) error {
	audioMsg := evt.Message.GetAudioMessage()
	if audioMsg == nil {
		return fmt.Errorf("audio message details not found")
	}

	// Example: assume audioMsg.URL contains the download link and audioMsg.FileLength is available.
	directPath := audioMsg.URL
	fileLength := audioMsg.FileLength

	// Download audio file (here using a simple HTTP GET; in production, use WhatsMeow’s media download if available)
	resp, err := http.Get(directPath)
	if err != nil {
		log.Printf("Failed to download audio: %v", err)
		return err
	}
	defer resp.Body.Close()

	// Create a temporary file in MESSAGES_DIR
	tempFile, err := os.CreateTemp(MESSAGES_DIR, fmt.Sprintf("audio-%d-*.webm", fileLength))
	if err != nil {
		log.Printf("Failed to create temp file: %v", err)
		return err
	}
	defer tempFile.Close()
	tempFilePath := tempFile.Name()

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		log.Printf("Failed to save audio file: %v", err)
		return err
	}
	log.Printf("Audio message downloaded and saved to: %s", tempFilePath)

	// Transcribe the audio using Groq (alternatively, you could call CfTranscribe)
	transcription, err := TranscribeAudioGroq(tempFilePath, WHISPER_PROMPT, "pt")
	if err != nil {
		log.Printf("Error during transcription: %v", err)
		// Optionally, send a reply with an error message here.
		return err
	}
	log.Println("Audio transcription completed.")

	// Remove the temporary audio file
	if err := os.Remove(tempFilePath); err != nil {
		log.Printf("Error removing temporary audio file: %v", err)
	} else {
		log.Printf("Temporary audio file removed: %s", tempFilePath)
	}

	// Prepare and send the reply (adjust based on WhatsMeow’s sending API)
	transcription = strings.TrimSpace(transcription)
	replyText := fmt.Sprintf("*Transcrição automática:*\n\n_%s_", transcription)
	chatID := evt.Info.Chat.ID // assuming Chat.ID is available

	if err := client.SendMessage(chatID, replyText); err != nil {
		log.Printf("Failed to send reply message: %v", err)
		return err
	}
	log.Println("Reply sent successfully.")

	return nil
}
