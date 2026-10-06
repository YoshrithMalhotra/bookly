import kotlinx.browser.document
import kotlinx.coroutines.launch
import kotlinx.html.*
import kotlinx.html.js.onChangeFunction
import kotlinx.html.js.onClickFunction
import kotlinx.html.js.onSubmitFunction
import org.w3c.dom.HTMLElement
import org.w3c.dom.HTMLInputElement

/** Public booking page at /b/{slug}: service → date → time → details → done. */
class BookingPage(private val root: HTMLElement, private val slug: String) {
    private var biz: PublicBusiness? = null
    private var service: Service? = null
    private var date = ""
    private var slots: List<String>? = null
    private var slot: String? = null
    private var error: String? = null
    private var busy = false
    private var result: BookingResult? = null

    // Keep what the customer typed across re-renders.
    private var customerName = ""
    private var customerPhone = ""
    private var optIn = true

    fun show() {
        setTitle("Book")
        root.render { div("page") { main("container narrow") { spinner() } } }
        scope.launch {
            try {
                val b = Api.get<PublicBusiness>("/api/businesses/$slug")
                biz = b
                setTitle("Book with ${b.name}")
                date = firstOpenDate(b)
                if (b.services.size == 1) selectService(b.services[0]) else render()
            } catch (e: ApiException) {
                if (e.status == 404) notFound(root, "business") else { error = e.message; render() }
            }
        }
    }

    private fun today(b: PublicBusiness) = Fmt.today(b.timezone)
    private fun lastDay(b: PublicBusiness) = Fmt.addDays(today(b), b.maxDaysAhead)
    private fun isOpen(b: PublicBusiness, d: String) = b.hours.any { it.weekday == Fmt.weekday(d) }

    private fun firstOpenDate(b: PublicBusiness): String {
        var d = today(b)
        repeat(14) {
            if (isOpen(b, d)) return d
            d = Fmt.addDays(d, 1)
        }
        return today(b)
    }

    private fun selectService(s: Service) {
        service = s; slot = null; error = null
        loadSlots()
    }

    private fun setDate(d: String) {
        val b = biz ?: return
        if (d < today(b) || d > lastDay(b)) return
        date = d; slot = null
        loadSlots()
    }

    private fun loadSlots() {
        val s = service ?: return
        slots = null
        render()
        scope.launch {
            try {
                slots = Api.get<Slots>("/api/businesses/$slug/slots?service_id=${s.id}&date=$date").slots
            } catch (e: ApiException) {
                error = e.message
                slots = emptyList()
            }
            render()
        }
    }

    private fun saveForm() {
        if (document.getElementById("name") != null) {
            customerName = inputValue("name")
            customerPhone = inputValue("phone")
            optIn = checked("optin")
        }
    }

    private fun submit() {
        val b = biz ?: return
        val s = service ?: return
        val start = slot ?: return
        saveForm()
        busy = true; error = null; render()
        scope.launch {
            try {
                result = Api.send<BookingRequest, BookingResult>(
                    "POST", "/api/businesses/${b.slug}/appointments",
                    BookingRequest(s.id, start, customerName, customerPhone, optIn),
                )
            } catch (e: ApiException) {
                error = e.message
                if (e.status == 409) {
                    // Someone else just took it: refresh the times.
                    error = "Sorry, someone just booked that time. Please pick another."
                    slot = null
                    busy = false
                    loadSlots()
                    return@launch
                }
            }
            busy = false
            render()
            document.getElementById(if (result != null) "done" else "details")?.asDynamic()?.scrollIntoView()
        }
    }

    private fun render() {
        val b = biz
        root.render {
            div("page") {
                main("container narrow") {
                    if (b == null) {
                        errorBox(error)
                        return@main
                    }
                    div("biz-header") {
                        h1 { +b.name }
                        p("muted") { +"Book an appointment" }
                    }
                    val r = result
                    if (r != null) {
                        confirmation(r)
                        return@main
                    }
                    if (!b.acceptingBookings) {
                        div("card") { p { +"${b.name} isn't taking online bookings right now. Please contact them directly." } }
                        return@main
                    }
                    if (b.services.isEmpty()) {
                        div("card") { p { +"${b.name} isn't taking online bookings yet." } }
                        return@main
                    }
                    serviceStep(b)
                    if (service != null) dateStep(b)
                    if (slot != null) detailsStep(b)
                }
                siteFooter { div { +"Powered by "; link("/", null, "Bookly") } }
            }
        }
    }

    private fun FlowContent.serviceStep(b: PublicBusiness) {
        section("card step") {
            h2 { span("step-n") { +"1" }; +"Choose a service" }
            div("choices") {
                for (s in b.services) {
                    button(type = ButtonType.button, classes = "choice" + if (s.id == service?.id) " selected" else "") {
                        attributes["aria-pressed"] = (s.id == service?.id).toString()
                        onClickFunction = { saveForm(); selectService(s) }
                        span("choice-title") { +s.name }
                        span("choice-meta") {
                            +Fmt.duration(s.durationMin)
                            if (s.price.toDoubleOrNull() != 0.0) +" · ${s.price}"
                        }
                    }
                }
            }
        }
    }

    private fun FlowContent.dateStep(b: PublicBusiness) {
        section("card step") {
            h2 { span("step-n") { +"2" }; +"Pick a time" }
            div("date-nav") {
                button(type = ButtonType.button, classes = "button ghost") {
                    disabled = date <= today(b)
                    attributes["aria-label"] = "Previous day"
                    onClickFunction = { saveForm(); setDate(Fmt.addDays(date, -1)) }
                    +"‹"
                }
                input(InputType.date) {
                    id = "date"; value = date; min = today(b); max = lastDay(b)
                    attributes["aria-label"] = "Date"
                    onChangeFunction = { e ->
                        val v = (e.target as HTMLInputElement).value
                        if (v.isNotEmpty()) { saveForm(); setDate(v) }
                    }
                }
                button(type = ButtonType.button, classes = "button ghost") {
                    disabled = date >= lastDay(b)
                    attributes["aria-label"] = "Next day"
                    onClickFunction = { saveForm(); setDate(Fmt.addDays(date, 1)) }
                    +"›"
                }
            }
            p("day-label") { +Fmt.day(date) }
            errorBox(if (slot == null) error else null)
            val s = slots
            when {
                s == null -> spinner("Finding free times…")
                !isOpen(b, date) -> p("muted") { +"Closed on ${Fmt.weekdayNames[Fmt.weekday(date)]}s. Try another day." }
                s.isEmpty() -> p("muted") { +"No free times left on this day. Try another day." }
                else -> div("slots") {
                    for (t in s) {
                        button(type = ButtonType.button, classes = "slot" + if (t == slot) " selected" else "") {
                            attributes["aria-pressed"] = (t == slot).toString()
                            onClickFunction = {
                                saveForm(); slot = t; error = null; render()
                                document.getElementById("details")?.asDynamic()?.scrollIntoView()
                            }
                            +Fmt.time(t, b.timezone)
                        }
                    }
                }
            }
            if (b.timezone != Fmt.browserTimezone()) {
                p("muted small") { +"Times are shown in ${b.timezone}." }
            }
        }
    }

    private fun FlowContent.detailsStep(b: PublicBusiness) {
        val s = service ?: return
        val start = slot ?: return
        section("card step") {
            id = "details"
            h2 { span("step-n") { +"3" }; +"Your details" }
            p("summary") { strong { +s.name }; +" · ${Fmt.dateLong(start, b.timezone)} at ${Fmt.time(start, b.timezone)}" }
            errorBox(error)
            form {
                onSubmitFunction = { e -> e.stop(); submit() }
                label {
                    +"Your name"
                    input(InputType.text) {
                        id = "name"; required = true; maxLength = "100"; value = customerName
                        attributes["autocomplete"] = "name"
                    }
                }
                label {
                    +"Mobile number"
                    input(InputType.tel) {
                        id = "phone"; required = true; value = customerPhone; placeholder = "+44 7700 900123"
                        attributes["autocomplete"] = "tel"
                    }
                }
                label("checkbox") {
                    input(InputType.checkBox) { id = "optin"; checked = optIn }
                    +"Send me a WhatsApp reminder before my appointment"
                }
                button(type = ButtonType.submit, classes = "button full large") {
                    disabled = busy
                    +(if (busy) "Booking…" else "Book ${Fmt.time(start, b.timezone)}")
                }
            }
        }
    }

    private fun FlowContent.confirmation(r: BookingResult) {
        val a = r.appointment
        val tz = r.business.timezone
        section("card done") {
            id = "done"
            div("tick") { +"✓" }
            h2 { +"You're booked!" }
            p("summary") { strong { +a.serviceName }; br; +"${Fmt.dateLong(a.startsAt, tz)} at ${Fmt.time(a.startsAt, tz)}" }
            if (a.whatsappOptIn) p { +"We'll send a WhatsApp reminder to ${a.customerPhone}." }
            p("muted") { +"Need to change plans? Keep this link to cancel your booking:" }
            div("copy-row") {
                input(InputType.text) { readonly = true; value = r.manageUrl; attributes["aria-label"] = "Your booking link" }
                button(type = ButtonType.button, classes = "button ghost") {
                    onClickFunction = { e ->
                        copyToClipboard(r.manageUrl)
                        (e.currentTarget as HTMLElement).textContent = "Copied"
                    }
                    +"Copy"
                }
            }
            link("/c/${r.manageToken}", "button ghost full", "View my booking")
        }
    }
}
