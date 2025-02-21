package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/cdproto/cdp"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorMagenta = "\033[35m"
)

// API Response Structures
type APIResponse struct {
	ScrapperConfig          []map[string]interface{} `json:"scrapper_config"`
	EducationBoardResults []map[string]interface{} `json:"educationboardresults_01"`
}

// Configuration variables from API
var (
	headless       = false
	startMaximized = true
	disableGPU     = false
	statusAPI      string
	matchingCellSelector string
	nameSelector         string
	gpaSelector          string
	resultSelector       string
	subjectScript        string
)




// Education Level Mappings
// ------------------------
// SSC/Dakhil/Equivalent  -> "ssc"
// JSC/JDC                -> "jsc"
// SSC/Dakhil             -> "ssc"
//
// Vocational & Specialized Secondary Certifications:
//
// SSC(Vocational)        -> "ssc_voc"
//
// Standard Higher Secondary Certifications:
//
// HSC/Alim               -> "hsc"
//
// Vocational & Specialized Higher Secondary Certifications:
//
// HSC(Vocational)        -> "hsc_voc"
// HSC(BM)                -> "hsc_hbm"
// Diploma in Commerce    -> "hsc_dic"
// Diploma in Business Studies -> "hsc"





// year selection between 1996 - 2025 (as of now)








var (
	exam  = ""
	year  = ""
	board = ""
	roll  = ""
	reg   = ""
)

func fetchConfig() error {
	resp, err := http.Get("https://akash-pf.vercel.app/api/eduboard_bd")
	if err != nil {
		return fmt.Errorf("failed to fetch config: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	var apiResponse APIResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return fmt.Errorf("failed to parse API response: %v", err)
	}

	// Parse scraper config
	for _, config := range apiResponse.ScrapperConfig {
		for k, v := range config {
			switch k {
			case "headless":
				headless = v.(bool)
			case "start-maximized":
				startMaximized = v.(bool)
			case "disable-gpu":
				disableGPU = v.(bool)
			}
		}
	}

	// Parse education board results config
	for _, config := range apiResponse.EducationBoardResults {
		for k, v := range config {
			switch k {
			case "matching_cell_01":
				matchingCellSelector = v.(string)
			case "students_name_cell":
				nameSelector = v.(string)
			case "students_gpa_cell":
				gpaSelector = v.(string)
			case "student_result_cell":
				resultSelector = v.(string)
			case "execution_subject_js_script":
				subjectScript = v.(string)
			case "status_api":
				statusAPI = v.(string)
			}
		}
	}

	if statusAPI != "enabled" {
		return fmt.Errorf("service unavailable (status_api: %s)", statusAPI)
	}

	return nil
}

func main() {
	log.SetFlags(0)

	// Fetch configuration from API
	if err := fetchConfig(); err != nil {
		log.Fatalf("%s%s%s", colorRed, err.Error(), colorReset)
	}

	// Validate form inputs (unchanged from original)
	if err := validateFormInputs(exam, year, board, roll, reg); err != nil {
		log.Fatalf("%sValidation failed: %v%s", colorRed, err, colorReset)
	}

	// Create context with config from API
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-gpu", disableGPU),
		chromedp.Flag("start-maximized", startMaximized),
	)
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	var screenshot []byte
	var captchaValue string

	err := chromedp.Run(ctx, chromedp.Tasks{
		chromedp.Navigate("http://www.educationboardresults.gov.bd/"),
		chromedp.Sleep(2 * time.Second),

		chromedp.WaitVisible(`#exam`, chromedp.ByID),
		chromedp.WaitVisible(`#year`, chromedp.ByID),
		chromedp.WaitVisible(`#board`, chromedp.ByID),
		chromedp.WaitVisible(`#roll`, chromedp.ByID),
		chromedp.WaitVisible(matchingCellSelector, chromedp.ByQuery),

		chromedp.ActionFunc(func(ctx context.Context) error {
			var nodes []*cdp.Node
			if err := chromedp.Nodes(matchingCellSelector, &nodes, chromedp.ByQueryAll).Do(ctx); err != nil {
				return err
			}
			if len(nodes) < 13 {
				return fmt.Errorf("insufficient matching cells found: %d", len(nodes))
			}
			nodeXPath := nodes[12].FullXPath()
			return chromedp.Text(nodeXPath, &captchaValue, chromedp.NodeVisible, chromedp.BySearch).Do(ctx)
		}),

		chromedp.SendKeys(`#exam`, exam, chromedp.ByID),
		chromedp.SendKeys(`#year`, year, chromedp.ByID),
		chromedp.SendKeys(`#board`, board, chromedp.ByID),
		chromedp.SendKeys(`#roll`, roll, chromedp.ByID),
		chromedp.SendKeys(`#reg`, reg, chromedp.ByID),

		chromedp.ActionFunc(func(ctx context.Context) error {
			solvedCaptcha := calculateCaptcha(captchaValue)
			fmt.Printf("%s[SOLVED CAPTCHA]: %s%s\n", colorYellow, solvedCaptcha, colorReset)
			return chromedp.Evaluate(fmt.Sprintf(`document.getElementById("value_s").value = "%s";`, solvedCaptcha), nil).Do(ctx)
		}),

		chromedp.FullScreenshot(&screenshot, 90),
		chromedp.Click(`#button2`, chromedp.ByID),
		chromedp.Sleep(5 * time.Second),
		chromedp.FullScreenshot(&screenshot, 90),

		chromedp.ActionFunc(func(ctx context.Context) error {
			var studentName, gpa, result, subjectsStr string

			if err := chromedp.Text(nameSelector, &studentName, chromedp.NodeVisible, chromedp.BySearch).Do(ctx); err != nil {
				return err
			}
			fmt.Printf("%s[STUDENT NAME]: %s%s\n", colorGreen, studentName, colorReset)

			if err := chromedp.Text(gpaSelector, &gpa, chromedp.NodeVisible, chromedp.BySearch).Do(ctx); err != nil {
				return err
			}
			fmt.Printf("%s[GPA]: %s%s\n", colorYellow, gpa, colorReset)

			if err := chromedp.Text(resultSelector, &result, chromedp.NodeVisible, chromedp.BySearch).Do(ctx); err != nil {
				return err
			}
			fmt.Printf("%s[RESULT]: %s%s\n", colorMagenta, result, colorReset)

			if err := chromedp.Evaluate(subjectScript, &subjectsStr).Do(ctx); err != nil {
				return err
			}

			fmt.Printf("%s[SUBJECTS]:\n", colorBlue)
			if subjectsStr == "" {
				fmt.Println("No subjects found")
			} else {
				for _, line := range strings.Split(subjectsStr, "\n") {
					fmt.Println(line)
				}
			}
			fmt.Print(colorReset)
			return nil
		}),
	})

	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("screenshot.png", screenshot, 0644); err != nil {
		log.Fatal(err)
	}
	log.Println("Screenshot saved as screenshot.png")
}

// calculateCaptcha parses a simple "num1 + num2" string and returns the sum as a string.
func calculateCaptcha(captcha string) string {
	parts := strings.Split(captcha, "+")
	if len(parts) != 2 {
		return ""
	}
	num1, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return ""
	}
	num2, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return ""
	}
	return strconv.Itoa(num1 + num2)
}

// Validate the form inputs
func validateFormInputs(exam, year, board, roll, reg string) error {
	if err := validateExam(exam); err != nil {
		return err
	}
	if err := validateYear(year); err != nil {
		return err
	}
	if err := validateBoard(board); err != nil {
		return err
	}
	if err := validateRoll(roll); err != nil {
		return err
	}
	if err := validateReg(reg); err != nil {
		return err
	}
	return nil
}

func validateExam(exam string) error {
	validExams := map[string]bool{
		"ssc": true, "jsc": true, "ssc_voc": true,
		"hsc": true, "hsc_voc": true, "hsc_hbm": true, "hsc_dic": true,
	}
	if !validExams[strings.ToLower(exam)] {
		return fmt.Errorf("%sinvalid exam value: %s%s", colorRed, exam, colorReset)
	}
	return nil
}

func validateYear(year string) error {
	y, err := strconv.Atoi(year)
	if err != nil {
		log.Fatalf("%syear must be numeric: %s%s\n", colorRed, year, colorReset)
		//return fmt.Errorf("Year must be numeric: %s%s%s", colorRed, year, colorReset)
	}
	if y < 1996 || y > 2025 {
		return fmt.Errorf("%syear %d is out of range (1996-2025)%s", colorRed, y, colorReset)
	}
	return nil
}

func validateBoard(board string) error {
	validBoards := map[string]bool{
		"barisal": true, "chittagong": true, "comilla": true,
		"dhaka": true, "dinajpur": true, "jessore": true,
		"mymensingh": true, "rajshahi": true, "sylhet": true,
		"madrasah": true, "tec": true, "dibs": true,
	}
	if !validBoards[strings.ToLower(board)] {
		return fmt.Errorf("%sinvalid board value: %s%s", colorRed, board, colorReset)
	}
	return nil
}

func validateRoll(roll string) error {
	if _, err := strconv.Atoi(roll); err != nil {
		return fmt.Errorf("%sroll must be numeric: %s%s", colorRed, roll, colorReset)
	}
	return nil
}

func validateReg(reg string) error {
	if _, err := strconv.Atoi(reg); err != nil {
		return fmt.Errorf("%sreg must be numeric: %s%s", colorRed, reg, colorReset)
	}
	return nil
}