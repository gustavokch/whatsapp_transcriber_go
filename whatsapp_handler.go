package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	// Example - for core WhatsApp types
	// Example - for media related types
	// Example - for connection/call related types)
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
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	clientLog := waLog.Stdout("Client", "DEBUG", true)

	setupDirectories()

	EXCLUDED_NUMBERS = loadExcludedNumbers("exclude.txt")
	log.Printf("Loaded excluded numbers: %v", EXCLUDED_NUMBERS)

	// Initialize WhatsMeow client with sqlite storage (adjust DSN as needed)
	// dbLog := log.New(os.Stdout, "DB: ", log.LstdFlags)
	// waLog := log.New(os.Stdout, "WhatsApp: ", log.LstdFlags)
	// waLog.Logger=logger.Info
	container, err := sqlstore.New("sqlite3", "file:db.sqlite3?_foreign_keys=on", dbLog)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		log.Fatalf("Failed to get device: %v", err)
	}
	client := whatsmeow.NewClient(deviceStore, clientLog)

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
	if evt.Info.MediaType == "audio" && evt.Message.AudioMessage != nil {
		// Get the media key and direct path
		audioMsg := evt.Message.AudioMessage
		mediaKey := audioMsg.GetMediaKey()
		directPath := audioMsg.GetDirectPath()

		// Define the file path where the audio will be saved
		savePath := filepath.Join("downloads", fmt.Sprintf("%s.ogg", evt.Info.ID))

		// Ensure the directory exists
		if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
			fmt.Println("Error creating directory:", err)
			return
		}

		// Save the file
		err = os.WriteFile(savePath, data, 0644)
		if err != nil {
			fmt.Println("Error saving audio file:", err)
			return
		}

		fmt.Println("Audio message saved to:", savePath)
	}
	// Skip group messages
	if evt.Info.IsGroup {
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
	audioMsg := client.DownloadToFile(evt.Message.GetAudioMessage(), File)
	if audioMsg == nil {
		return fmt.Errorf("audio message details not found")
	}

	// Example: assume audioMsg.URL contains the download link and audioMsg.FileLength is available.

	// Download audio file (here using a simple HTTP GET; in production, use WhatsMeow’s media download if available)

	//	Create a temporary file in MESSAGES_DIR
	tempFile, err := os.CreateTemp(MESSAGES_DIR, fmt.Sprintf("audio-%d-*.webm"))
	if err != nil {
		log.Printf("Failed to create temp file: %v", err)
		return err
	}
	defer tempFile.Close()
	tempFilePath := tempFile.Name()

	if _, err := io.Copy(tempFile, audioMsg); err != nil {
		log.Printf("Failed to save audio file: %v", err)
		return err
	}
	log.Printf("Audio message downloaded and saved to: %s", audioMsg)

	// Transcribe the audio using Groq (alternatively, you could call CfTranscribe)
	transcription, err := TranscribeAudioGroq(audioMsg, WHISPER_PROMPT, "pt")
	if err != nil {
		log.Printf("Error during transcription: %v", err)
		// Optionally, send a reply with an error message here.
		return err
	}
	log.Println("Audio transcription completed.")

	// Remove the temporary audio file
	// if err := os.Remove(tempFilePath); err != nil {
	// 	log.Printf("Error removing temporary audio file: %v", err)
	// } else {
	// 	log.Printf("Temporary audio file removed: %s", tempFilePath)
	// }

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
