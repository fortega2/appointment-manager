// Converts UTC timestamps rendered by the server into each visitor's own
// local timezone/locale (via Intl), and converts local datetime-local form
// input back into UTC before submit. The server never assumes a timezone.
(function () {
	function pad(n) {
		return String(n).padStart(2, "0");
	}

	// The chosen UI language, not the browser's. Only the timezone still
	// comes from the browser.
	function uiLocale() {
		return document.documentElement.lang || navigator.language;
	}

	function formatDate(d) {
		return new Intl.DateTimeFormat(uiLocale(), {
			year: "numeric",
			month: "2-digit",
			day: "2-digit",
		}).format(d);
	}

	function formatTime(d) {
		return new Intl.DateTimeFormat(uiLocale(), {
			hour: "2-digit",
			minute: "2-digit",
			hour12: false,
		}).format(d);
	}

	function toLocalInputValue(utcISO) {
		if (!utcISO) return "";
		const d = new Date(utcISO);
		if (Number.isNaN(d.getTime())) return "";
		return (
			`${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
			`T${pad(d.getHours())}:${pad(d.getMinutes())}`
		);
	}

	function localInputValueToUTCISO(localValue) {
		if (!localValue) return "";
		const d = new Date(localValue);
		if (Number.isNaN(d.getTime())) return "";
		return d.toISOString();
	}

	// <tr data-utc-start data-utc-end> rows (slot table, appointment table):
	// rewrites whichever [data-role] cells are present and refreshes the
	// local date/start/end dataset used by the client-side week/time filters.
	function renderTimeRanges(root) {
		root.querySelectorAll("[data-utc-start][data-utc-end]").forEach(function (el) {
			const start = new Date(el.dataset.utcStart);
			const end = new Date(el.dataset.utcEnd);
			if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) return;

			const startText = formatTime(start);
			const endText = formatTime(end);
			const dateText = formatDate(start);

			const dateEl = el.querySelector('[data-role="date"]');
			const timeEl = el.querySelector('[data-role="time"]');
			const datetimeEl = el.querySelector('[data-role="datetime"]');
			const endTimeEl = el.querySelector('[data-role="end-time"]');

			if (dateEl) dateEl.textContent = dateText;
			if (timeEl) timeEl.textContent = `${startText} - ${endText}`;
			if (datetimeEl) datetimeEl.textContent = `${dateText} ${startText}`;
			if (endTimeEl) endTimeEl.textContent = endText;

			el.dataset.date = `${start.getFullYear()}-${pad(start.getMonth() + 1)}-${pad(start.getDate())}`;
			el.dataset.start = startText;
			el.dataset.end = endText;
		});
	}

	// <option data-utc-start data-utc-end data-prof-name> in the appointment
	// form's slot picker: rewrites the option label in the visitor's locale.
	function renderSlotOptions(root) {
		root.querySelectorAll("option[data-utc-start][data-utc-end]").forEach(function (opt) {
			const start = new Date(opt.dataset.utcStart);
			const end = new Date(opt.dataset.utcEnd);
			if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) return;

			const honorific = opt.closest("select")?.dataset.honorific || "";
			const professional = [honorific, opt.dataset.profName].filter(Boolean).join(" ");
			opt.textContent = `${formatDate(start)} ${formatTime(start)}-${formatTime(end)} · ${professional}`;
		});
	}

	// form[data-slot-form]: prefills the datetime-local inputs (browser-local)
	// from the server-provided UTC value in data-utc-value.
	function prefillSlotForms(root) {
		root.querySelectorAll('form[data-slot-form] [data-role="start-local"], form[data-slot-form] [data-role="end-local"]').forEach(
			function (input) {
				if (input.value || !input.dataset.utcValue) return;
				input.value = toLocalInputValue(input.dataset.utcValue);
			}
		);
	}

	function init(root) {
		root = root?.querySelectorAll ? root : document;
		renderTimeRanges(root);
		renderSlotOptions(root);
		prefillSlotForms(root);
	}

	init(document);
	document.addEventListener("htmx:afterSettle", function (evt) {
		init(evt.target);
	});

	// htmx has already serialized the form into evt.detail.parameters by the
	// time this fires, but the request hasn't been sent yet: the documented
	// hook for rewriting outgoing values, so we don't race htmx's own submit
	// handling the way a plain "submit" listener would.
	document.body.addEventListener("htmx:configRequest", function (evt) {
		const form = evt.detail.elt.closest?.("form[data-slot-form]");
		if (!form) return;

		const startLocal = form.querySelector('[data-role="start-local"]');
		const endLocal = form.querySelector('[data-role="end-local"]');
		if (!startLocal || !endLocal) return;

		evt.detail.parameters.start_time = localInputValueToUTCISO(startLocal.value);
		evt.detail.parameters.end_time = localInputValueToUTCISO(endLocal.value);
	});

	window.AppTimezone = { init: init };
})();
