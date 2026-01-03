package cmd

import "context"

func runDownloadAllFormats(ctx context.Context, observer DownloadObserver, useProgressBar bool) (downloadSummary, error) {
	formats, err := parseFormats(format)
	if err != nil {
		return downloadSummary{}, err
	}
	if len(formats) == 1 {
		format = formats[0]
		return runDownload(ctx, observer, useProgressBar)
	}

	originalFormat := format
	aggregated := downloadSummary{}
	observerWrapper := observer
	if observer != nil {
		observerWrapper = newMultiFormatObserver(observer).Observe
	}

	for idx, nextFormat := range formats {
		format = nextFormat
		summary, err := runDownload(ctx, observerWrapper, useProgressBar)
		if idx == 0 {
			aggregated.Mode = summary.Mode
		}
		aggregated.Downloaded += summary.Downloaded
		aggregated.Skipped += summary.Skipped
		aggregated.Failed += summary.Failed
		if err != nil {
			format = originalFormat
			return aggregated, err
		}
	}
	format = originalFormat
	return aggregated, nil
}

type multiFormatObserver struct {
	observer          DownloadObserver
	planTotal         int
	planSkipped       int
	planRefreshed     int
	summaryDownloaded int
	summarySkipped    int
	summaryFailed     int
}

func newMultiFormatObserver(observer DownloadObserver) *multiFormatObserver {
	return &multiFormatObserver{observer: observer}
}

func (m *multiFormatObserver) Observe(event DownloadEvent) {
	switch event.Type {
	case DownloadEventPlan:
		m.planTotal += event.Total
		m.planSkipped += event.Skipped
		m.planRefreshed += event.Refreshed
		event.Total = m.planTotal
		event.Skipped = m.planSkipped
		event.Refreshed = m.planRefreshed
	case DownloadEventSummary:
		m.summaryDownloaded += event.Downloaded
		m.summarySkipped += event.Skipped
		m.summaryFailed += event.Failed
		event.Downloaded = m.summaryDownloaded
		event.Skipped = m.summarySkipped
		event.Failed = m.summaryFailed
	}
	m.observer(event)
}
