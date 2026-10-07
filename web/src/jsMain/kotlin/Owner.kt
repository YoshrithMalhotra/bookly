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

    private var newBooking = false   // "New booking" form open
    private var moving: Long? = null // appointment being rescheduled

    // Services tab (also used by the new booking form)
    private var services: List<Service>? = null
    private var editing: Long? = null // service id being edited; 0 = new

    // Hours tab
    private var hours: List<Hours>? = null
    private var timeOff: List<TimeOff>? = null

    // Insights tab
    private var stats: StatsResponse? = null

    // Billing (shown as a banner on every tab, and its own tab)
    private var billing: Billing? = null

    private val tabs = listOf(
        "appointments" to "Appointments",
        "services" to "Services",
        "hours" to "Opening hours",
        "settings" to "Settings",
        "insights" to "Insights",
        "billing" to "Billing",
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
                billing = Api.get("/api/owner/billing")
                if (tab == "billing" && kotlinx.browser.window.location.search.contains("checkout=success")) {
                    notice = "Thanks! Your subscription is being activated; this page will update in a moment."
                    // The Stripe webhook may land a second after the redirect.
                    kotlinx.coroutines.delay(2500)
                    billing = Api.get("/api/owner/billing")
                }
                load()
            } catch (e: ApiException) {
                if (e.status == 401) Router.go("/login", replace = true) else { error = e.message; render() }
            }
        }
    }

    private suspend fun load() {
        try {
            when (tab) {
                "appointments" -> {
                    day = Api.get("/api/owner/appointments?date=$date")
                    if (services == null) services = Api.get("/api/owner/services")
                }
                "services" -> services = Api.get("/api/owner/services")
                "hours" -> {
                    hours = Api.get("/api/owner/hours")
                    timeOff = Api.get("/api/owner/time-off")
                }
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
                if (success != null) notice = success
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
                    billingBanner()
                    verifyBanner(b)
                    errorBox(error)
                    okBox(notice)
                    when (tab) {
                        "appointments" -> appointmentsTab(b)
                        "services" -> servicesTab()
                        "hours" -> hoursTab()
                        "settings" -> settingsTab(b)
                        "insights" -> insightsTab()
                        "billing" -> billingTab()
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
        div("row-between") {
            h2 { +(if (date == today) "Today · " else ""); +Fmt.day(date) }
            if (!newBooking) button(type = ButtonType.button, classes = "button") {
                onClickFunction = { newBooking = true; notice = null; render() }
                +"New booking"
            }
        }
        if (newBooking) newBookingForm(b)
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
                        div {
                            strong { +a.customerName }; +" "; statusBadge(a.status)
                            if (a.source == "owner") span("badge badge-cancelled") { +"Added by you" }
                        }
                        div("muted") { +a.serviceName }
                        if (a.notes.isNotEmpty()) div("note small") { +"“${a.notes}”" }
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
                        if (moving == a.id) moveForm(a, b)
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
                                } else {
                                    if (a.status == "booked") action("Confirm", "confirmed")
                                    button(type = ButtonType.button, classes = "button ghost small") {
                                        disabled = busy
                                        onClickFunction = { moving = if (moving == a.id) null else a.id; render() }
                                        +"Move"
                                    }
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

    private fun saveNewBooking(b: Business) {
        val d = rawInputValue("nb-date")
        val t = rawInputValue("nb-time")
        if (d.isEmpty() || t.isEmpty()) { error = "Pick a date and time."; render(); return }
        val req = BookingRequest(
            serviceId = selectValue("nb-service").toLongOrNull() ?: 0,
            startsAt = Fmt.zonedToIso(d, t, b.timezone),
            customerName = inputValue("nb-name"),
            customerPhone = inputValue("nb-phone"),
            whatsappOptIn = checked("nb-optin"),
            notes = inputValue("nb-notes"),
        )
        act("Booking added.") {
            Api.send<BookingRequest, OwnerBookingResult>("POST", "/api/owner/appointments", req)
            newBooking = false
            date = d
            day = Api.get("/api/owner/appointments?date=$date")
        }
    }

    private fun FlowContent.newBookingForm(b: Business) {
        val svcs = services.orEmpty().filter { it.active }
        form(classes = "card") {
            onSubmitFunction = { e -> e.stop(); saveNewBooking(b) }
            h3 { +"New booking" }
            p("muted small") { +"For phone bookings and walk-ins. You can book outside opening hours; double bookings are still blocked." }
            if (svcs.isEmpty()) {
                p { +"Add a service first."; +" "; link("/owner/services", null, "Services") }
                return@form
            }
            div("grid3") {
                label {
                    +"Service"
                    select { id = "nb-service"; for (s in svcs) option { value = s.id.toString(); +"${s.name} (${Fmt.duration(s.durationMin)})" } }
                }
                label { +"Date"; input(InputType.date) { id = "nb-date"; required = true; value = date } }
                label { +"Start time"; input(InputType.time) { id = "nb-time"; required = true; step = "300" } }
            }
            div("grid3") {
                label { +"Customer name"; input(InputType.text) { id = "nb-name"; required = true; maxLength = "100" } }
                label { +"Mobile number"; input(InputType.tel) { id = "nb-phone"; required = true; placeholder = "+44 7700 900123" } }
                label { +"Note"; input(InputType.text) { id = "nb-notes"; maxLength = "500"; placeholder = "Optional, only you see it" } }
            }
            label("checkbox") {
                input(InputType.checkBox) { id = "nb-optin" }
                +"Customer agreed to a WhatsApp reminder"
            }
            div("actions") {
                button(type = ButtonType.submit, classes = "button") { disabled = busy; +"Add booking" }
                button(type = ButtonType.button, classes = "button ghost") { onClickFunction = { newBooking = false; render() }; +"Cancel" }
            }
        }
    }

    private fun FlowContent.moveForm(a: Appointment, b: Business) {
        form(classes = "move-form") {
            onSubmitFunction = { e ->
                e.stop()
                val d = rawInputValue("mv-date")
                val t = rawInputValue("mv-time")
                if (d.isNotEmpty() && t.isNotEmpty()) {
                    act("Appointment moved. The customer's reminder will show the new time.") {
                        Api.send<RescheduleRequest, Appointment>("POST", "/api/owner/appointments/${a.id}/reschedule",
                            RescheduleRequest(Fmt.zonedToIso(d, t, b.timezone)))
                        moving = null
                        day = Api.get("/api/owner/appointments?date=$date")
                    }
                }
            }
            input(InputType.date) { id = "mv-date"; required = true; value = Fmt.dateIn(a.startsAt, b.timezone); attributes["aria-label"] = "New date" }
            input(InputType.time) { id = "mv-time"; required = true; step = "300"; value = Fmt.clock(a.startsAt, b.timezone); attributes["aria-label"] = "New time" }
            button(type = ButtonType.submit, classes = "button small") { disabled = busy; +"Move" }
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
                        div("muted small") { +Fmt.duration(s.durationMin); +" · "; +Fmt.money(s.price, biz?.currency ?: "GBP") }
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
        timeOffSection()
    }

    private fun addTimeOff() {
        val b = biz ?: return
        val fromD = rawInputValue("to-from-date"); val fromT = rawInputValue("to-from-time").ifEmpty { "00:00" }
        val toD = rawInputValue("to-to-date"); val toT = rawInputValue("to-to-time").ifEmpty { "23:59" }
        if (fromD.isEmpty() || toD.isEmpty()) { error = "Pick the first and last day."; render(); return }
        val t = TimeOff(startsAt = Fmt.zonedToIso(fromD, fromT, b.timezone), endsAt = Fmt.zonedToIso(toD, toT, b.timezone),
            reason = inputValue("to-reason"))
        act {
            val r = Api.send<TimeOff, TimeOffResult>("POST", "/api/owner/time-off", t)
            timeOff = Api.get("/api/owner/time-off")
            notice = if (r.clashingAppointments > 0)
                "Time off added. ${r.clashingAppointments} existing appointment(s) fall in this period: move or cancel them from Appointments."
            else "Time off added. Customers can't book during it."
        }
    }

    private fun FlowContent.timeOffSection() {
        val b = biz ?: return
        val list = timeOff ?: return
        h2 { +"Time off" }
        p("muted") { +"Holidays, breaks and days off. Customers can't book during these times." }
        if (list.isNotEmpty()) {
            ul("services") {
                for (t in list) li("card service") {
                    div {
                        strong { +"${Fmt.dateLong(t.startsAt, b.timezone)} ${Fmt.clock(t.startsAt, b.timezone)}" }
                        +" → "
                        strong { +"${Fmt.dateLong(t.endsAt, b.timezone)} ${Fmt.clock(t.endsAt, b.timezone)}" }
                        if (t.reason.isNotEmpty()) div("muted small") { +t.reason }
                    }
                    button(type = ButtonType.button, classes = "button ghost small danger-text") {
                        disabled = busy
                        onClickFunction = {
                            act("Time off removed.") {
                                Api.request("DELETE", "/api/owner/time-off/${t.id}")
                                timeOff = Api.get("/api/owner/time-off")
                            }
                        }
                        +"Remove"
                    }
                }
            }
        }
        form(classes = "card") {
            onSubmitFunction = { e -> e.stop(); addTimeOff() }
            h3 { +"Add time off" }
            div("grid4") {
                label { +"From"; input(InputType.date) { id = "to-from-date"; required = true; value = date.ifEmpty { Fmt.today(b.timezone) } } }
                label { +"at"; input(InputType.time) { id = "to-from-time"; value = "00:00" } }
                label { +"Until"; input(InputType.date) { id = "to-to-date"; required = true; value = date.ifEmpty { Fmt.today(b.timezone) } } }
                label { +"at"; input(InputType.time) { id = "to-to-time"; value = "23:59" } }
            }
            label { +"Reason (only you see it)"; input(InputType.text) { id = "to-reason"; maxLength = "200"; placeholder = "Holiday" } }
            button(type = ButtonType.submit, classes = "button") { disabled = busy; +"Add time off" }
        }
    }

    // Settings

    private fun saveSettings() {
        val update = BusinessUpdate(inputValue("set-name"), selectValue("set-tz"), inputValue("set-review"), selectValue("set-currency"))
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
                +"Currency"
                select {
                    id = "set-currency"
                    val list = if (b.currency in Fmt.currencies) Fmt.currencies else listOf(b.currency) + Fmt.currencies
                    for (c in list) option { value = c; selected = c == b.currency; +"$c (${Fmt.money("0", c).replace(Regex("[0-9.,\\s]"), "")})" }
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

        h2 { +"Login email" }
        form(classes = "card") {
            onSubmitFunction = { e -> e.stop(); changeEmail() }
            p {
                +"Currently "; strong { +b.ownerEmail }
                if (!b.emailVerified) span("badge badge-no_show") { +"Not confirmed" }
            }
            div("grid3") {
                label { +"New email"; input(InputType.email) { id = "em-new"; required = true; attributes["autocomplete"] = "email" } }
                label { +"Your password"; input(InputType.password) { id = "em-password"; required = true; attributes["autocomplete"] = "current-password" } }
            }
            button(type = ButtonType.submit, classes = "button") { disabled = busy; +"Change email" }
        }

        h2 { +"Your data" }
        div("card") {
            p { +"Download every appointment as a spreadsheet (CSV) for your records or to move to another system." }
            a(href = "/api/owner/export.csv", classes = "button ghost") { attributes["download"] = ""; +"Download appointments" }
        }

        h2 { +"Change password" }
        form(classes = "card") {
            onSubmitFunction = { e -> e.stop(); changePassword() }
            label {
                +"Current password"
                input(InputType.password) { id = "pw-current"; required = true; attributes["autocomplete"] = "current-password" }
            }
            label {
                +"New password"
                input(InputType.password) {
                    id = "pw-new"; required = true; minLength = "10"; maxLength = "72"
                    attributes["autocomplete"] = "new-password"
                }
                small("muted") { +"At least 10 characters. Other devices will be logged out." }
            }
            button(type = ButtonType.submit, classes = "button") { disabled = busy; +"Change password" }
        }

        h2 { +"Delete account" }
        form(classes = "card danger-zone") {
            onSubmitFunction = { e -> e.stop(); deleteAccount() }
            p {
                +"This permanently deletes your business, services, appointments and customer details, "
                +"and cancels your subscription. It can't be undone."
            }
            label {
                +"Type your password to confirm"
                input(InputType.password) { id = "del-password"; required = true; attributes["autocomplete"] = "current-password" }
            }
            button(type = ButtonType.submit, classes = "button danger") { disabled = busy; +"Delete my account" }
        }
    }

    private fun changeEmail() {
        val req = ChangeEmailRequest(inputValue("em-new"), rawInputValue("em-password"))
        act("Email changed. We've sent a confirmation link to the new address.") {
            biz = Api.send<ChangeEmailRequest, Business>("PUT", "/api/owner/email", req)
        }
    }

    private fun FlowContent.verifyBanner(b: Business) {
        if (b.emailVerified) return
        div("alert banner") {
            +"Please confirm your email (${b.ownerEmail}) using the link we sent, so you can reset your password if you ever need to. "
            a(href = "#") {
                onClickFunction = { e ->
                    e.preventDefault()
                    act("We've sent a new confirmation link.") { Api.post("/api/owner/email/resend") }
                }
                +"Resend"
            }
        }
    }

    private fun changePassword() {
        val req = ChangePasswordRequest(rawInputValue("pw-current"), rawInputValue("pw-new"))
        busy = true; error = null; notice = null; render()
        scope.launch {
            try {
                notice = Api.send<ChangePasswordRequest, MessageResponse>("PUT", "/api/owner/password", req).message
            } catch (e: ApiException) {
                handle(e)
            }
            busy = false
            render()
        }
    }

    private fun deleteAccount() {
        val pw = rawInputValue("del-password")
        if (!window.confirm("Delete your account and all its data permanently?")) return
        busy = true; error = null; notice = null; render()
        scope.launch {
            try {
                Api.request("DELETE", "/api/owner/account", Api.json.encodeToString(DeleteAccountRequest(pw)))
                window.alert("Your account has been deleted.")
                Router.go("/", replace = true)
            } catch (e: ApiException) {
                busy = false
                handle(e)
                render()
            }
        }
    }

    // Billing

    private fun daysLeft(iso: String): Int =
        kotlin.math.ceil((Date(iso).getTime() - Date().getTime()) / 86_400_000.0).toInt()

    private fun FlowContent.billingBanner() {
        val bl = billing ?: return
        if (!bl.enabled || tab == "billing") return
        val trialDays = daysLeft(bl.trialEndsAt)
        when {
            !bl.active -> div("alert error banner") {
                +"Your booking page is paused: customers can't book until you subscribe. "
                link("/owner/billing", null, "Subscribe now")
            }
            bl.status == "past_due" -> div("alert error banner") {
                +"Your last payment failed. Please update your card to keep taking bookings. "
                link("/owner/billing", null, "Fix billing")
            }
            bl.status == "trial" && trialDays <= 7 -> div("alert banner") {
                +"Your free trial ends in $trialDays day${if (trialDays == 1) "" else "s"}. "
                link("/owner/billing", null, "Subscribe to keep your booking page open")
            }
        }
    }

    private fun goToStripe(path: String) {
        busy = true; error = null; notice = null; render()
        scope.launch {
            try {
                val r = Api.request("POST", path).let { Api.json.decodeFromString<RedirectUrl>(it) }
                window.location.href = r.url
            } catch (e: ApiException) {
                busy = false
                handle(e)
                render()
            }
        }
    }

    private fun FlowContent.billingTab() {
        val bl = billing ?: return spinner()
        val tz = biz?.timezone ?: "UTC"
        h2 { +"Billing" }
        if (!bl.enabled) {
            div("card") { p("muted") { +"Billing isn't set up on this server, so your booking page is always open." } }
            return
        }
        div("card") {
            val label = when (bl.status) {
                "trial" -> if (bl.active) "Free trial" else "Trial ended"
                "active" -> "Active"
                "trialing" -> "Active (trial)"
                "past_due" -> "Payment failed"
                "canceled" -> "Cancelled"
                "unpaid" -> "Unpaid"
                "incomplete", "incomplete_expired" -> "Payment not completed"
                "paused" -> "Paused"
                else -> bl.status
            }
            p {
                strong { +"Plan: " }
                +label
                if (bl.priceLabel.isNotEmpty()) span("muted") { +" · ${bl.priceLabel}" }
            }
            when {
                bl.status == "trial" && bl.active ->
                    p { +"Your trial ends ${Fmt.dateLong(bl.trialEndsAt, tz)} (${daysLeft(bl.trialEndsAt)} days left). Subscribe any time; you won't lose any trial days' bookings." }
                !bl.active -> p("danger-text") { +"Your booking page is paused. Existing appointments and reminders still work, but customers can't make new bookings." }
                bl.currentPeriodEnd != null ->
                    p { +(if (bl.status == "canceled") "Access ended " else "Next renewal: ") ; +Fmt.dateLong(bl.currentPeriodEnd, tz) }
            }
            div("actions") {
                val subscribed = bl.status in listOf("active", "trialing", "past_due")
                if (!subscribed) {
                    button(type = ButtonType.button, classes = "button") {
                        disabled = busy
                        onClickFunction = { goToStripe("/api/owner/billing/checkout") }
                        +(if (busy) "Opening checkout…" else "Subscribe")
                    }
                }
                if (bl.hasCustomer) {
                    button(type = ButtonType.button, classes = "button ghost") {
                        disabled = busy
                        onClickFunction = { goToStripe("/api/owner/billing/portal") }
                        +"Manage billing, card & invoices"
                    }
                }
            }
            p("muted small") { +"Payments are processed securely by Stripe. You can cancel any time." }
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
