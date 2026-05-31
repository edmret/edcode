package banner

import "fmt"

const Logo = `________  _______          ______    ______   _______   ________
|        \|       \        /      \  /      \ |       \ |        \
| $$$$$$$$| $$$$$$$\      |  $$$$$$\|  $$$$$$\| $$$$$$$\| $$$$$$$$
| $$__    | $$  | $$      | $$   \$$| $$  | $$| $$  | $$| $$
| $$  \   | $$  | $$      | $$      | $$  | $$| $$  | $$| $$  \
| $$$$$   | $$  | $$      | $$   __ | $$  | $$| $$  | $$| $$$$$
| $$_____ | $$__/ $$      | $$__/  \| $$__/ $$| $$__/ $$| $$_____
| $$     \| $$    $$       \$$    $$ \$$    $$| $$    $$| $$     \
 \$$$$$$$$ \$$$$$$$         \$$$$$$   \$$$$$$  \$$$$$$$  \$$$$$$$$`

const Tagline = "your AI agent harness"

const colorGreen = "\033[32m"
const colorReset = "\033[0m"

func Print() {
	fmt.Print(colorGreen + Logo + colorReset + "\n")
}

func PrintWithTagline() {
	Print()
	fmt.Printf("  %s\n\n", Tagline)
}

func PrintMinimal() {
	fmt.Println("  EdCode — " + Tagline)
}
