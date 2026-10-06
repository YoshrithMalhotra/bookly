import kotlinx.browser.window
import kotlinx.coroutines.launch
import kotlinx.html.*
import kotlinx.html.js.onClickFunction
import org.w3c.dom.HTMLElement

/** Customer's view of their booking at /c/{token}, with cancel. */
class ManagePage(private val root: HTMLElement, private val token: String) {
    private var booking: ManagedBooking? = null
    private var error: String? = null
    private var message: String? = null
    private var busy = false

    fun show() {
        setTitle("Your booking")
        root.render { div("page") { main("container narrow") { spinner() } } }
        scope.launch {
            try {
                booking = Api.get("/api/manage/$token")
                render()
            } catch (e: ApiException) {
                if (e.status == 404) notFound(root, "booking") else { error = e.message; render() }
            }
        }
    }

    private fun cancel() {
        if (!window.confirm("Cancel this appointment?")) return
        busy = true; error = null; render()
        scope.launch {
            try {
                booking = Api.request("POST", "/api/manage/$token/cancel").let { Api.json.decodeFromString(it) }
                message = "Your appointment has been cancelled."
            } catch (e: ApiException) {
                error = e.message
            }
            busy = false
            render()
        }
    }

    private fun render() {
        val m = booking
        root.render {
            div("page") {
                main("container narrow") {
                    errorBox(error)
                    okBox(message)
                    if (m == null) return@main
                    val a = m.appointment
                    val tz = m.business.timezone
                    div("biz-header") { h1 { +m.business.name } }
                    section("card") {
                        h2 { +a.serviceName }
                        p("summary") { +"${Fmt.dateLong(a.startsAt, tz)} at ${Fmt.time(a.startsAt, tz)}" }
                        p { +"Booked for ${a.customerName}" }
                        p { statusBadge(a.status) }
                        if (m.canCancel) {
                            button(type = ButtonType.button, classes = "button danger full") {
                                disabled = busy
                                onClickFunction = { cancel() }
                                +(if (busy) "Cancelling…" else "Cancel appointment")
                            }
                        }
                        link("/b/${m.business.slug}", "button ghost full", if (a.status == "cancelled") "Book a new time" else "Book another appointment")
                    }
                }
                siteFooter { div { +"Powered by "; link("/", null, "Bookly") } }
            }
        }
    }
}

fun FlowOrPhrasingContent.statusBadge(status: String) {
    val label = when (status) {
        "booked" -> "Booked"
        "confirmed" -> "Confirmed"
        "cancelled" -> "Cancelled"
        "done" -> "Done"
        "no_show" -> "No-show"
        else -> status
    }
    span("badge badge-$status") { +label }
}
