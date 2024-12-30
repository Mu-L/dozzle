package web

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"encoding/json"

	"io"
	"net/http"
	"runtime"

	"time"

	"github.com/amir20/dozzle/internal/auth"
	"github.com/amir20/dozzle/internal/docker"
	"github.com/amir20/dozzle/internal/support/search"
	support_web "github.com/amir20/dozzle/internal/support/web"
	"github.com/amir20/dozzle/internal/utils"
	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"

	"github.com/rs/zerolog/log"
)

func (h *handler) fetchLogsBetweenDates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-jsonl; charset=UTF-8")

	from, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("from"))
	to, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("to"))
	id := chi.URLParam(r, "id")

	var stdTypes docker.StdType
	if r.URL.Query().Has("stdout") {
		stdTypes |= docker.STDOUT
	}
	if r.URL.Query().Has("stderr") {
		stdTypes |= docker.STDERR
	}

	if stdTypes == 0 {
		http.Error(w, "stdout or stderr is required", http.StatusBadRequest)
		return
	}

	usersFilter := h.config.Filter
	if h.config.Authorization.Provider != NONE {
		user := auth.UserFromContext(r.Context())
		if user.ContainerFilter.Exists() {
			usersFilter = user.ContainerFilter
		}
	}

	containerService, err := h.multiHostService.FindContainer(hostKey(r), id, usersFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	buffer := utils.NewRingBuffer[*docker.LogEvent](500)
	delta := max(to.Sub(from), time.Second*3)

	var regex *regexp.Regexp
	if r.URL.Query().Has("filter") {
		regex, err = search.ParseRegex(r.URL.Query().Get("filter"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	onlyComplex := r.URL.Query().Has("jsonOnly")
	everything := r.URL.Query().Has("everything")
	if everything {
		from = time.Time{}
		to = time.Now()
	}

	minimum := 0
	if r.URL.Query().Has("minimum") {
		minimum, err = strconv.Atoi(r.URL.Query().Get("minimum"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if minimum < 0 || minimum > buffer.Size {
			http.Error(w, errors.New("minimum must be between 0 and buffer size").Error(), http.StatusBadRequest)
			return
		}
	}

	levels := make(map[string]struct{})
	for _, level := range r.URL.Query()["levels"] {
		levels[level] = struct{}{}
	}

	lastSeenId := uint32(0)
	if r.URL.Query().Has("lastSeenId") {
		to = to.Add(50 * time.Millisecond) // Add a little buffer to ensure we get the last event
		num, err := strconv.ParseUint(r.URL.Query().Get("lastSeenId"), 10, 32)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lastSeenId = uint32(num)
	}

	encoder := json.NewEncoder(w)

	for {
		if buffer.Len() > minimum {
			break
		}

		buffer.Clear()

		events, err := containerService.LogsBetweenDates(r.Context(), from, to, stdTypes)
		if err != nil {
			log.Error().Err(err).Msg("error fetching logs")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for event := range events {
			if everything {
				if err := encoder.Encode(event); err != nil {
					log.Error().Err(err).Msg("error encoding log event")
				}
				continue
			}

			if onlyComplex {
				if _, ok := event.Message.(string); ok {
					continue
				}
			}

			if regex != nil {
				if !search.Search(regex, event) {
					continue
				}
			}

			if _, ok := levels[event.Level]; !ok {
				continue
			}

			if lastSeenId != 0 && event.Id == lastSeenId {
				log.Debug().Uint32("lastSeenId", lastSeenId).Msg("found last seen id")
				break
			}

			buffer.Push(event)
		}

		if everything || from.Before(containerService.Container.Created) {
			break
		}

		from = from.Add(-delta)
		delta = delta * 2
	}

	log.Debug().Int("buffer_size", buffer.Len()).Msg("sending logs to client")

	for _, event := range buffer.Data() {
		if err := encoder.Encode(event); err != nil {
			log.Error().Err(err).Msg("error encoding log event")
			return
		}
	}
}

func (h *handler) streamContainerLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	h.streamLogsForContainers(w, r, func(container *docker.Container) bool {
		return container.ID == id && container.Host == hostKey(r)
	})
}

func (h *handler) streamLogsMerged(w http.ResponseWriter, r *http.Request) {
	idsSplit := strings.Split(chi.URLParam(r, "ids"), ",")

	ids := make(map[string]bool)
	for _, id := range idsSplit {
		ids[id] = true
	}

	h.streamLogsForContainers(w, r, func(container *docker.Container) bool {
		return ids[container.ID] && container.Host == hostKey(r)
	})
}

func (h *handler) streamServiceLogs(w http.ResponseWriter, r *http.Request) {
	service := chi.URLParam(r, "service")
	h.streamLogsForContainers(w, r, func(container *docker.Container) bool {
		return container.State == "running" && container.Labels["com.docker.swarm.service.name"] == service
	})
}

func (h *handler) streamGroupedLogs(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")

	h.streamLogsForContainers(w, r, func(container *docker.Container) bool {
		return container.State == "running" && container.Group == group
	})
}

func (h *handler) streamStackLogs(w http.ResponseWriter, r *http.Request) {
	stack := chi.URLParam(r, "stack")

	h.streamLogsForContainers(w, r, func(container *docker.Container) bool {
		return container.State == "running" && container.Labels["com.docker.stack.namespace"] == stack
	})
}

func (h *handler) streamLogsForContainers(w http.ResponseWriter, r *http.Request, containerFilter ContainerFilter) {
	var stdTypes docker.StdType
	if r.URL.Query().Has("stdout") {
		stdTypes |= docker.STDOUT
	}
	if r.URL.Query().Has("stderr") {
		stdTypes |= docker.STDERR
	}

	if stdTypes == 0 {
		http.Error(w, "stdout or stderr is required", http.StatusBadRequest)
		return
	}

	sseWriter, err := support_web.NewSSEWriter(r.Context(), w)
	if err != nil {
		log.Error().Err(err).Msg("error creating sse writer")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	usersFilter := h.config.Filter
	if h.config.Authorization.Provider != NONE {
		user := auth.UserFromContext(r.Context())
		if user.ContainerFilter.Exists() {
			usersFilter = user.ContainerFilter
		}
	}

	existingContainers, errs := h.multiHostService.ListAllContainersFiltered(usersFilter, containerFilter)
	if len(errs) > 0 {
		log.Warn().Err(errs[0]).Msg("error while listing containers")
	}

	absoluteTime := time.Time{}
	var regex *regexp.Regexp
	liveLogs := make(chan *docker.LogEvent)
	events := make(chan *docker.ContainerEvent, 1)
	backfill := make(chan []*docker.LogEvent)

	levels := make(map[string]struct{})
	for _, level := range r.URL.Query()["levels"] {
		levels[level] = struct{}{}
	}

	if r.URL.Query().Has("filter") {
		var err error
		regex, err = search.ParseRegex(r.URL.Query().Get("filter"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		absoluteTime = time.Now()

		go func() {
			minimum := 50
			delta := -10 * time.Second
			to := absoluteTime
			for minimum > 0 {
				events := make([]*docker.LogEvent, 0)
				stillRunning := false
				for _, container := range existingContainers {
					containerService, err := h.multiHostService.FindContainer(container.Host, container.ID, usersFilter)

					if err != nil {
						log.Error().Err(err).Msg("error while finding container")
						return
					}

					if to.Before(containerService.Container.Created) {
						continue
					}

					logs, err := containerService.LogsBetweenDates(r.Context(), to.Add(delta), to, stdTypes)
					if err != nil {
						log.Error().Err(err).Msg("error while fetching logs")
						return
					}

					for log := range logs {
						if _, ok := levels[log.Level]; !ok {
							continue
						}
						if search.Search(regex, log) {
							events = append(events, log)
						}
					}

					stillRunning = true
				}

				if !stillRunning {
					return
				}

				to = to.Add(delta)
				delta *= 2
				minimum -= len(events)
				sort.Slice(events, func(i, j int) bool {
					return events[i].Timestamp < events[j].Timestamp
				})
				if len(events) > 0 {
					backfill <- events
				}
			}
		}()
	}

	streamLogs := func(container docker.Container) {
		containerService, err := h.multiHostService.FindContainer(container.Host, container.ID, usersFilter)
		if err != nil {
			log.Error().Err(err).Msg("error while finding container")
			return
		}
		start := utils.Max(absoluteTime, container.StartedAt)
		err = containerService.StreamLogs(r.Context(), start, stdTypes, liveLogs)
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Debug().Str("container", container.ID).Msg("streaming ended")
				events <- &docker.ContainerEvent{ActorID: container.ID, Name: "container-stopped", Host: container.Host}
			} else if !errors.Is(err, context.Canceled) {
				log.Error().Err(err).Str("container", container.ID).Msg("unknown error while streaming logs")
			}
		}
	}

	for _, container := range existingContainers {
		go streamLogs(container)
	}

	newContainers := make(chan docker.Container)
	h.multiHostService.SubscribeContainersStarted(r.Context(), newContainers, containerFilter)

	ticker := time.NewTicker(5 * time.Second)
	sseWriter.Ping()
loop:
	for {
		select {
		case logEvent := <-liveLogs:
			if regex != nil {
				if !search.Search(regex, logEvent) {
					continue
				}
			}

			if _, ok := levels[logEvent.Level]; !ok {
				continue
			}
			sseWriter.Message(logEvent)
		case container := <-newContainers:
			if _, err := h.multiHostService.FindContainer(container.Host, container.ID, usersFilter); err == nil {
				events <- &docker.ContainerEvent{ActorID: container.ID, Name: "container-started", Host: container.Host}
				go streamLogs(container)
			}

		case event := <-events:
			log.Debug().Str("event", event.Name).Str("container", event.ActorID).Msg("received event")
			if err := sseWriter.Event("container-event", event); err != nil {
				log.Error().Err(err).Msg("error encoding container event")
			}

		case backfillEvents := <-backfill:
			if err := sseWriter.Event("logs-backfill", backfillEvents); err != nil {
				log.Error().Err(err).Msg("error encoding container event")
			}

		case <-ticker.C:
			sseWriter.Ping()

		case <-r.Context().Done():
			break loop
		}
	}

	if e := log.Debug(); e.Enabled() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		e.Str("allocated", humanize.Bytes(m.Alloc)).
			Str("totalAllocated", humanize.Bytes(m.TotalAlloc)).
			Str("system", humanize.Bytes(m.Sys)).
			Int("routines", runtime.NumGoroutine()).
			Msg("runtime mem stats")
	}
}
