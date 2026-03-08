package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// UIController manages global UI elements like toasts and the loading spinner.
type UIController struct {
	spinner        spinner.Model
	syncing        bool
	fetchingDetail bool
	toastMessage   string
	resourceFilter string
	styles         Styles
	width          int
	height         int
	toastTimeout   time.Duration
}

func NewUIController(styles Styles) UIController {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))),
	)

	return UIController{
		spinner:      s,
		styles:       styles,
		toastTimeout: 3 * time.Second,
	}
}

func (c *UIController) SetSize(width, height int) {
	c.width = width
	c.height = height
}

func (c *UIController) SetStyles(styles Styles) {
	c.styles = styles
}

func (c *UIController) Update(msg tea.Msg) (UIController, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if c.syncing || c.fetchingDetail {
			c.spinner, cmd = c.spinner.Update(msg)
			return *c, cmd
		}
	case clearStatusMsg:
		c.toastMessage = ""
	}
	return *c, nil
}

func (c *UIController) SetToast(msg string) tea.Cmd {
	c.toastMessage = msg
	return tea.Tick(c.toastTimeout, func(_ time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (c *UIController) SetSyncing(syncing bool) tea.Cmd {
	c.syncing = syncing
	if syncing {
		return c.spinner.Tick
	}
	return nil
}

func (c *UIController) SetFetching(fetching bool) tea.Cmd {
	c.fetchingDetail = fetching
	if fetching {
		return c.spinner.Tick
	}
	return nil
}

func (c *UIController) SetResourceFilter(filter string) {
	c.resourceFilter = filter
}

// View composes the overlays on top of the provided base content.
func (c *UIController) View(baseContent string, showDetail bool, viewportScrollPercent float64, viewportHeight int, totalLines int) string {
	if c.width <= 0 || c.height <= 0 {
		return baseContent
	}

	// Base layer
	base := lipgloss.NewLayer(baseContent).X(0).Y(0).Z(0)
	cptr := lipgloss.NewCompositor(base)

	// 1. Toast Notification
	if c.toastMessage != "" {
		toast := c.styles.Toast.Render(c.toastMessage)
		toastWidth := lipgloss.Width(toast)
		toastY := c.height - 2
		if toastY < 0 { toastY = 0 }
		
		toastLayer := lipgloss.NewLayer(toast).
			X((c.width - toastWidth) / 2).
			Y(toastY).
			Z(100)
		cptr.AddLayers(toastLayer)
	}

	// 2. Scrollbar for Detail View
	if showDetail && !c.fetchingDetail && viewportScrollPercent >= 0 {
		scrollbarHeight := viewportHeight
		
		thumbHeight := 3 // Minimal thumb size
		if totalLines > 0 {
			thumbHeight = int(float64(scrollbarHeight) * (float64(scrollbarHeight) / float64(totalLines)))
			if thumbHeight < 3 { thumbHeight = 3 }
		}
		if thumbHeight > scrollbarHeight { thumbHeight = scrollbarHeight }
		
		thumbPos := int(float64(scrollbarHeight-thumbHeight) * viewportScrollPercent)
		
		thumb := c.styles.ScrollbarThumb.
			Width(1).
			Height(thumbHeight).
			Render(" ")
		
		sbLayer := lipgloss.NewLayer(thumb).
			X(c.width - 2).
			Y(4 + thumbPos). // Start after header
			Z(50)
		cptr.AddLayers(sbLayer)
	}

	// 3. Filter Chip (Floating top-right)
	if c.resourceFilter != "" {
		chipText := fmt.Sprintf(" FILTER: %s ", strings.ToUpper(c.resourceFilter))
		chip := c.styles.FilterChip.Render(chipText)
		chipWidth := lipgloss.Width(chip)
		
		chipLayer := lipgloss.NewLayer(chip).
			X(c.width - chipWidth - 2).
			Y(0).
			Z(110)
		cptr.AddLayers(chipLayer)
	}

	return cptr.Render()
}

func (c *UIController) RenderSpinner() string {
	if c.syncing || c.fetchingDetail {
		return c.spinner.View()
	}
	return ""
}
