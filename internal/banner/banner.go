package banner

import "fmt"

const Logo = `
 ________  _______          ______    ______   _______   ________
|        \|       \        /      \  /      \ |       \ |        \
| $$$$$$$$| $$$$$$$\      |  $$$$$$\|  $$$$$$\| $$$$$$$\| $$$$$$$$
| $$__    | $$  | $$      | $$   \$$| $$  | $$| $$  | $$| $$
| $$  \   | $$  | $$      | $$      | $$  | $$| $$  | $$| $$  \
| $$$$$   | $$  | $$      | $$   __ | $$  | $$| $$  | $$| $$$$$
| $$_____ | $$__/ $$      | $$__/  \| $$__/ $$| $$__/ $$| $$_____
| $$     \| $$    $$       \$$    $$ \$$    $$| $$    $$| $$     \
 \$$$$$$$$ \$$$$$$$         \$$$$$$   \$$$$$$  \$$$$$$$  \$$$$$$$$`

const Tagline = "your AI agent harness"

func Print() {
	fmt.Println(Logo)
}

func PrintWithTagline() {
	Print()
	fmt.Println(Tagline)
	fmt.Println()
}

func PrintMinimal() {
	fmt.Println("EdCode - " + Tagline)
}
