package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/templates"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

func main() {
	// Load configuration from environment
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}

	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}

	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}

	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "postgres"
	}

	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "openv"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "./uploads"
	}

	// Create uploads directory if it doesn't exist
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		log.Fatalf("Failed to create uploads directory: %v", err)
	}

	// Build DSN
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	// Connect to database
	db, err := postgres.Connect(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize schema
	if err := postgres.InitSchema(db); err != nil {
		log.Fatalf("Failed to initialize schema: %v", err)
	}

	// Create repositories
	artifactRepo := postgres.NewArtifactRepository(db)
	linkRepo := postgres.NewLinkRepository(db)
	projectRepo := postgres.NewProjectRepository(db)
	attachmentRepo := postgres.NewAttachmentRepository(db)
	projectInfoRepo := postgres.NewProjectInfoRepository(db)
	baselineRepo := postgres.NewBaselineRepository(db)
	templateRepo := postgres.NewTemplateRepository(db)

	// Create services
	artifactService := artifacts.NewDefaultService(artifactRepo)
	linkService := links.NewDefaultService(linkRepo)
	
	// Wire link service to artifact service for versioning triggers
	linkService.SetArtifactService(artifactService)
	
	projectService := projects.NewService(projectRepo)
	attachmentService := attachments.NewDefaultService(attachmentRepo)
	baselineService := baselines.NewService(baselineRepo)
	exportService := exports.NewService(artifactService, linkService, attachmentService, projectInfoRepo, projectService)
	reportService := reports.NewService(exportService, baselineService)
	templateService := templates.NewService(templateRepo, exportService)
	if err := templateService.SeedDefaults(); err != nil {
		log.Printf("Failed to seed templates: %v", err)
	}

	// Create handler
	handler := api.NewHandler(artifactService, linkService, projectService, attachmentService, exportService, baselineService, reportService, templateService, uploadsDir)

	// Setup router
	router := mux.NewRouter()
	router.Use(api.ContentTypeMiddleware)
	handler.RegisterRoutes(router)

	// Wrap router with CORS handler at the HTTP level
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		router.ServeHTTP(w, r)
	})

	// Start server
	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, corsHandler); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
