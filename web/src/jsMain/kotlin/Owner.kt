import kotlinx.browser.document
import kotlinx.browser.window
import kotlinx.coroutines.launch
import kotlinx.html.*
import kotlinx.html.js.onChangeFunction
import kotlinx.html.js.onClickFunction
import kotlinx.html.js.onSubmitFunction
import org.w3c.dom.HTMLElement
import org.w3c.dom.HTMLInputElement
import kotlin.js.Date

/** Owner dashboard at /owner/{tab}. */
class OwnerPage(private val root: HTMLElement, private val tab: String) {
    private var biz: Business? = null
    private var error: String? = null
    private var notice: String? = null
    private var busy = false

    // Appointments tab
    private var date = ""
    private var day: DayAppointments? = null
    private var openMessages: Long? = null
    private var messages: List<Message>? = null

    // Services tab
    private var services: List<Service>? = null
    private var editing: Long? = null // service id being edited; 0 = new

    // Hours tab
    private var hours: List<Hours>? = null

    // Insights tab
    private var stats: StatsResponse? = null

    private val tabs = listOf(
        "appointments" to "Appointments",
        "services" to "Services",
        "hours" to "Opening hours",
        "settings" to "Settings",
        "insights" to "Insights",
    )

    fun show() {
        if (tabs.none { it.first == tab }) return Router.go("/owner", replace = true)
        setTitle(tabs.first { it.first == tab }.second)
        root.render { div("page") { main("container") { spinner() } } }
        scope.launch {
            try {
                val b = Api.get<Business>("/api/owner/business")
                biz = b
                date = Fmt.today(b.timezone)
                load()
            } catch (e: ApiException) {
                if (e.status == 401) Router.go("/login", replace = true) else { error = e.message; render() }
            }
        }
    }

    private suspend fun load() {
        try {
            when (tab) {
                "appointments" -> day = Api.get("/api/owner/appointments?date=$date")
                "services" -> services = Api.get("/api/owner/services")
                "hours" -> hours = Api.get("/api/owner/hours")
                "insights" -> stats = Api.get("/api/owner/stats")
            }
        } catch (e: ApiException) {
            handle(e)
        }
        render()
    }

    private fun handle(e: ApiException) {
        if (e.status == 401) Router.go("/login", replace = true) else error = e.message
    }

    /** Run an API action with busy/error handling, then re-render. */
    private fun act(success: String? = null, block: suspend () -> Unit) {
        busy = true; error = null; notice = null; render()
        scope.launch {
            try {
                block()
                notice = success
            } catch (e: ApiException) {
                handle(e)
            }
            busy = false
            render()
        }
    }

    private fun logout() {
        scope.launch {
            try { Api.post("/api/logout") } catch (_: ApiException) {}
            Router.go("/")
        }
    }

    private fun render() {
        val b = biz ?: run {
            root.render { div("page") { main("container") { errorBox(error) } } }
            return
        }
        val bookingUrl = "${window.location.origin}/b/${b.slug}"
        root.render {
            div("page") {
                topBar {
                    span("muted hide-sm") { +b.name }
                    button(type = ButtonType.button, classes = "button ghost") {
                        onClickFunction = { logout() }
                        +"Log out"
                    }
                }
                main("container") {
                    div("share card") {
                        div {
                            strong { +"Your booking link" }
                            p("muted small") { +"Share it on Instagram, Google Maps or WhatsApp." }
                        }
                        div("copy-row") {
                            input(InputType.text) { readonly = true; value = bookingUrl; attributes["aria-label"] = "Booking link" }
                            button(type = ButtonType.button, classes = "button ghost") {
                                onClickFunction = { e ->
                                    copyToClipboard(bookingUrl)
                                    (e.currentTarget as HTMLElement).textContent = "Copied"
                                }
                                +"Copy"
                            }
                            a(href = "/b/${b.slug}", target = "_blank", classes = "button ghost") { +"Open" }
                        }
                    }
                    nav("tabs") {
                        for ((key, label) in tabs) link("/owner/$key", if (key == tab) "tab active" else "tab", label)
                    }
                    errorBox(error)
                    okBox(notice)
                    when (tab) {
                        "appointments" -> appointmentsTab(b)
                        "services" -> servicesTab()
                        "hours" -> hoursTab()
                        "settings" -> settingsTab(b)
                        "insights" -> insightsTab()
                    }
                }
            }
        }
    }

    // Appointments

    private fun setDate(d: String) {
        date = d; day = null; openMessages = null
        render()
        scope.launch { load() }
    }

    private fun setStatus(a: Appointment, status: String) {
        if (status == "cancelled" && !window.confirm("Cancel ${a.customerName}'s appointment? Their reminder won't be sent.")) return
        act {
            val updated = Api.send<StatusRequest, Appointment>("POST", "/api/owner/appointments/${a.id}/status", StatusRequest(status))
            day = day?.let { d -> d.copy(appointments = d.appointments.map { if (it.id == updated.id) updated else it }) }
        }
    }

    private fun toggleMessages(a: Appointment) {
        if (openMessages == a.id) {
            openMessages = null; render(); return
        }
        openMessages = a.id; messages = null; render()
        scope.launch {
            try { messages = Api.get("/api/owner/appointments/${a.id}/messages") } catch (e: ApiException) { handle(e) }
            render()
        }
    }

    private fun FlowContent.appointmentsTab(b: Business) {
        val today = Fmt.today(b.timezone)
        div("date-nav") {
            button(type = ButtonType.button, classes = "button ghost") {
                attributes["aria-label"] = "Previous day"
                onClickFunction = { setDate(Fmt.addDays(date, -1)) }
                +"‹"
            }
            input(InputType.date) {
                value = date
                attributes["aria-label"] = "Date"
                onChangeFunction = { e -> (e.target as HTMLInputElement).value.takeIf { it.isNotEmpty() }?.let { setDate(it) } }
            }
            button(type = ButtonType.button, classes = "button ghost") {
                attributes["aria-label"] = "Next day"
                onClickFunction = { setDate(Fmt.addDays(date, 1)) }
                +"›"
            }
            if (date != today) {
                button(type = ButtonType.button, classes = "button ghost") { onClickFunction = { setDate(today) }; +"Today" }
            }
        }
        h2 { +(if (date == today) "Today · " else "") ; +Fmt.day(date) }
        val d = day ?: return spinner()
        if (d.appointments.isEmpty()) {
            div("card empty") { p("muted") { +"No appointments on this day." } }
            return
        }
        val now = Date().getTime()
        ul("appointments") {
            for (a in d.appointments) {
                val started = Date(a.startsAt).getTime() <= now
                li("card appt" + if (a.status == "cancelled") " is-cancelled" else "") {
                    div("appt-time") {
                        strong { +Fmt.time(a.startsAt, b.timezone) }
                        span("muted small") { +"– ${Fmt.time(a.endsAt, b.timezone)}" }
                    }
                    div("appt-main") {
                        div { strong { +a.customerName }; +" "; statusBadge(a.status) }
                        div("muted") { +a.serviceName }
                        div("small") {
                            a(href = "tel:${a.customerPhone}") { +a.customerPhone }
                            +" · "
                            a(href = "https://wa.me/${a.customerPhone.removePrefix("+")}", target = "_blank") { +"WhatsApp" }
                            if (a.whatsappOptIn) {
                                +" · "
                                a(href = "#", classes = "small") {
                                    onClickFunction = { e -> e.preventDefault(); toggleMessages(a) }
                                    +"Messages"
                                }
                            }
                        }
                        if (openMessages == a.id) messagesList(b)
                    }
                    div("appt-actions") {
                        fun action(label: String, status: String, cls: String = "button ghost small") {
                            button(type = ButtonType.button, classes = cls) {
                                disabled = busy
                                onClickFunction = { setStatus(a, status) }
                                +label
                            }
                        }
                        when (a.status) {
                            "booked", "confirmed" -> {
                                if (started) {
                                    action("Done", "done", "button small")
                                    action("No-show", "no_show")
                                } else if (a.status == "booked") {
                                    action("Confirm", "confirmed")
                                }
                                action("Cancel", "cancelled", "button ghost small danger-text")
                            }
                            "done" -> action("Mark no-show", "no_show")
                            "no_show" -> action("Mark done", "done")
                        }
                    }
                }
            }
        }
    }

    private fun FlowContent.messagesList(b: Business) {
        val ms = messages ?: return spinner()
        if (ms.isEmpty()) { p("muted small") { +"No messages." }; return }
        ul("messages small") {
            for (m in ms) {
                li {
                    +(if (m.type == "reminder") "Reminder" else "Review request")
                    +" · "
                    +when (m.status) {
                        "sent" -> "sent ${Fmt.dateLong(m.sentAt ?: m.sendAt, b.timezone)} ${Fmt.time(m.sentAt ?: m.sendAt, b.timezone)}"
                        "pending" -> "scheduled ${Fmt.dateLong(m.sendAt, b.timezone)} ${Fmt.time(m.sendAt, b.timezone)}"
                        else -> m.status
                    }
                    m.lastError?.let { if (m.status != "sent") span("danger-text") { +" ($it)" } }
                }
            }
        }
    }

    // Services

    private fun saveService(id: Long) {
        val sv = Service(
            id = id,
            name = inputValue("svc-name"),
            durationMin = inputValue("svc-duration").toIntOrNull() ?: 0,
            price = inputValue("svc-price"),
            active = checked("svc-active"),
        )
        act("Saved.") {
            if (id == 0L) Api.send<Service, Service>("POST", "/api/owner/services", sv)
            else Api.send<Service, Service>("PUT", "/api/owner/services/$id", sv)
            services = Api.get("/api/owner/services")
            editing = null
        }
    }

    private fun FlowContent.serviceForm(s: Service) {
        form(classes = "card service-form") {
            onSubmitFunction = { e -> e.stop(); saveService(s.id) }
            div("grid3") {
                label { +"Name"; input(InputType.text) { id = "svc-name"; required = true; maxLength = "100"; value = s.name } }
                label {
                    +"Duration (minutes)"
                    input(InputType.number) { id = "svc-duration"; required = true; min = "5"; max = "720"; step = "5"; value = s.durationMin.toString() }
                }
                label {
                    +"Price"
                    input(InputType.text) { id = "svc-price"; value = s.price; attributes["inputmode"] = "decimal"; placeholder = "25.00" }
                }
            }
            label("checkbox") {
                input(InputType.checkBox) { id = "svc-active"; checked = s.active }
                +"Customers can book this"
            }
            div("actions") {
                button(type = ButtonType.submit, classes = "button") { disabled = busy; +"Save" }
                button(type = ButtonType.button, classes = "button ghost") { onClickFunction = { editing = null; render() }; +"Cancel" }
            }
        }
    }

    private fun FlowContent.servicesTab() {
        val list = services ?: return spinner()
        div("row-between") {
            h2 { +"Services" }
            if (editing != 0L) button(type = ButtonType.button, classes = "button") {
                onClickFunction = { editing = 0L; notice = null; render() }
                +"Add service"
            }
        }
        if (editing == 0L) serviceForm(Service(name = "", durationMin = 30, price = ""))
        if (list.isEmpty() && editing != 0L) {
            div("card empty") { p { +"Add your first service so customers can book." } }
        }
        ul("services") {
            for (s in list) {
                if (editing == s.id) { li { serviceForm(s) }; continue }
                li("card service" + if (!s.active) " is-retired" else "") {
                    div {
                        strong { +s.name }
                        if (!s.active) span("badge badge-cancelled") { +"Hidden" }
                        div("muted small") { +Fmt.duration(s.durationMin); +" · "; +s.price }
                    }
                    button(type = ButtonType.button, classes = "button ghost small") {
                        onClickFunction = { editing = s.id; notice = null; render() }
                        +"Edit"
                    }
                }
            }
        }
    }

    // Opening hours

    private fun saveHours() {
        val list = (0..6).mapNotNull { d ->
            if (!checked("open-$d")) null else Hours(d, rawInputValue("opens-$d"), rawInputValue("closes-$d"))
        }
        act("Opening hours saved.") {
            hours = Api.send<List<Hours>, List<Hours>>("PUT", "/api/owner/hours", list)
        }
    }

    private fun FlowContent.hoursTab() {
        val hs = hours ?: return spinner()
        h2 { +"Opening hours" }
        p("muted") { +"Customers can book any time inside these hours. Appointments start every service-length from opening time." }
        form(classes = "card hours") {
            onSubmitFunction = { e -> e.stop(); saveHours() }
            // Monday first.
            for (d in listOf(1, 2, 3, 4, 5, 6, 0)) {
                val h = hs.firstOrNull { it.weekday == d }
                div("hours-row") {
                    label("checkbox") {
                        input(InputType.checkBox) {
                            id = "open-$d"; checked = h != null
                            onChangeFunction = { e ->
                                val on = (e.target as HTMLInputElement).checked
                                listOf("opens-$d", "closes-$d").forEach { (document.getElementById(it) as HTMLInputElement).disabled = !on }
                            }
                        }
                        +Fmt.weekdayNames[d]
                    }
                    input(InputType.time) { id = "opens-$d"; value = h?.opens ?: "09:00"; disabled = h == null; attributes["aria-label"] = "${Fmt.weekdayNames[d]} opens" }
                    span("muted") { +"to" }
                    input(InputType.time) { id = "closes-$d"; value = h?.closes ?: "17:00"; disabled = h == null; attributes["aria-label"] = "${Fmt.weekdayNames[d]} closes" }
                }
            }
            button(type = ButtonType.submit, classes = "button") { disabled = busy; +"Save hours" }
        }
    }

    // Settings

    private fun saveSettings() {
        val update = BusinessUpdate(inputValue("set-name"), selectValue("set-tz"), inputValue("set-review"))
        act("Settings saved.") {
            biz = Api.send<BusinessUpdate, Business>("PUT", "/api/owner/business", update)
        }
    }

    private fun FlowContent.settingsTab(b: Business) {
        h2 { +"Settings" }
        form(classes = "card") {
            onSubmitFunction = { e -> e.stop(); saveSettings() }
            label { +"Business name"; input(InputType.text) { id = "set-name"; required = true; maxLength = "100"; value = b.name } }
            label {
                +"Timezone"
                select {
                    id = "set-tz"
                    val zones = Fmt.timezones().let { if (b.timezone in it) it else listOf(b.timezone) + it }
                    for (tz in zones) option { value = tz; selected = tz == b.timezone; +tz }
                }
            }
            label {
                +"Google review link"
                input(InputType.url) { id = "set-review"; value = b.googleReviewUrl; placeholder = "https://g.page/r/…/review" }
                small("muted") {
                    +"Find it in your Google Business Profile under “Ask for reviews”. "
                    +"Customers you mark as done get this link by WhatsApp. Leave empty to turn review requests off."
                }
            }
            p("muted small") { +"Booking link: /b/${b.slug} · Login: ${b.ownerEmail}" }
            button(type = ButtonType.submit, classes = "button") { disabled = busy; +"Save settings" }
        }
    }

    // Insights

    private fun FlowContent.insightsTab() {
        val r = stats ?: return spinner()
        val s = r.stats
        val attended = s.done + s.noShow
        h2 { +"Last ${r.days} days" }
        div("stats") {
            stat("Visits", s.done.toString())
            stat("No-shows", s.noShow.toString())
            stat("No-show rate", if (attended == 0) "–" else "${(s.noShow * 1000 / attended) / 10.0}%")
            stat("Cancelled", s.cancelled.toString())
            stat("Reminders sent", s.remindersSent.toString())
            stat("Review requests sent", s.reviewsSent.toString())
        }
        if (s.unmarked > 0) {
            p("muted") { +"${s.unmarked} past appointments aren't marked done or no-show yet. Marking them keeps these numbers right and sends review requests." }
        }
    }

    private fun FlowContent.stat(label: String, value: String) {
        div("card stat") {
            div("stat-value") { +value }
            div("muted small") { +label }
        }
    }
}
