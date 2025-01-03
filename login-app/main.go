// main.go
package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
)

// PageData holds data to pass to the HTML template
type PageData struct {
	Message     string
	MessageType string // "error" or "success"
}

func main() {
	// Parse the templates
	templates, err := template.ParseFiles(filepath.Join("templates", "login.html"))
	if err != nil {
		log.Fatalf("Error parsing templates: %v", err)
	}

	// Handle the root path
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{}
		templates.ExecuteTemplate(w, "login.html", data)
	})

	// Handle the login form submission
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		// Parse form data
		if err := r.ParseForm(); err != nil {
			log.Printf("Error parsing form: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		username := r.FormValue("username")
		password := r.FormValue("password")

		fmt.Println("Information Received: ", username, password)
	})

	// Serve static files if needed (e.g., CSS, JS)
	// http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Start the server
	addr := ":8080"
	log.Printf("Server is running at http://localhost%s/", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
