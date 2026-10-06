import kotlinx.browser.document
import kotlinx.browser.window
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.MainScope
import kotlinx.html.*
import kotlinx.html.dom.append
import kotlinx.html.js.onClickFunction
import org.w3c.dom.HTMLElement
import org.w3c.dom.HTMLInputElement
import org.w3c.dom.HTMLSelectElement
import org.w3c.dom.events.Event

val scope: CoroutineScope = MainScope()

/** Replace an element's content with freshly built HTML. kotlinx.html escapes all text. */
fun HTMLElement.render(block: TagConsumer<HTMLElement>.() -> Unit) {
    innerHTML = ""
    append { block() }
}

fun inputValue(id: String): String = (document.getElementById(id) as? HTMLInputElement)?.value?.trim() ?: ""
fun rawInputValue(id: String): String = (document.getElementById(id) as? HTMLInputElement)?.value ?: ""
fun checked(id: String): Boolean = (document.getElementById(id) as? HTMLInputElement)?.checked ?: false
fun selectValue(id: String): String = (document.getElementById(id) as? HTMLSelectElement)?.value ?: ""

/** A link that navigates without reloading the page. */
fun FlowOrPhrasingContent.link(href: String, classes: String? = null, text: String) {
    a(href = href, classes = classes) {
        onClickFunction = { e ->
            e.preventDefault()
            Router.go(href)
        }
        +text
    }
}

fun FlowContent.errorBox(message: String?) {
    if (message != null) div("alert error") { attributes["role"] = "alert"; +message }
}

fun FlowContent.okBox(message: String?) {
    if (message != null) div("alert ok") { attributes["role"] = "status"; +message }
}

fun FlowContent.spinner(label: String = "Loading…") {
    div("loading") { span("spinner") {}; +label }
}

fun FlowContent.topBar(right: FlowContent.() -> Unit = {}) {
    header("topbar") {
        link("/", "brand", "Bookly")
        div("topbar-right") { right() }
    }
}

fun Event.stop() {
    preventDefault()
    stopPropagation()
}

fun setTitle(t: String) {
    document.title = if (t.isEmpty()) "Bookly" else "$t · Bookly"
}

fun copyToClipboard(text: String) {
    window.navigator.asDynamic().clipboard?.writeText(text)
}

/** Path-based routing; the Go server returns index.html for every non-API path. */
object Router {
    private val root get() = document.getElementById("app") as HTMLElement

    fun start() {
        window.onpopstate = { show() }
        show()
    }

    fun go(path: String, replace: Boolean = false) {
        if (replace) window.history.replaceState(null, "", path) else window.history.pushState(null, "", path)
        show()
        window.scrollTo(0.0, 0.0)
    }

    private fun show() {
        val path = window.location.pathname.trimEnd('/').ifEmpty { "/" }
        val parts = path.split("/").filter { it.isNotEmpty() }
        when {
            path == "/" -> HomePage(root).show()
            path == "/login" -> LoginPage(root).show()
            path == "/signup" -> SignupPage(root).show()
            parts.size == 2 && parts[0] == "b" -> BookingPage(root, parts[1]).show()
            parts.size == 2 && parts[0] == "c" -> ManagePage(root, parts[1]).show()
            path == "/forgot" -> ForgotPage(root).show()
            parts.size == 2 && parts[0] == "reset" -> ResetPage(root, parts[1]).show()
            path == "/privacy" -> LegalPage(root, privacy = true).show()
            path == "/terms" -> LegalPage(root, privacy = false).show()
            parts.firstOrNull() == "owner" -> OwnerPage(root, parts.getOrNull(1) ?: "appointments").show()
            else -> notFound(root)
        }
    }
}

fun notFound(root: HTMLElement, what: String = "page") {
    setTitle("Not found")
    root.render {
        div("page") {
            topBar()
            main("container narrow") {
                h1 { +"We couldn't find that $what" }
                p { +"Check the link and try again." }
                link("/", "button", "Go home")
            }
        }
    }
}

/** Deployment settings from /api/config, loaded once. */
object Config {
    private var cached: AppConfig? = null

    suspend fun get(): AppConfig = cached ?: try {
        Api.get<AppConfig>("/api/config").also { cached = it }
    } catch (e: ApiException) {
        AppConfig()
    }
}

fun FlowContent.siteFooter(extra: FlowContent.() -> Unit = {}) {
    footer("footer") {
        extra()
        div("footer-links") {
            link("/terms", null, "Terms")
            +" · "
            link("/privacy", null, "Privacy")
        }
    }
}

fun main() {
    Router.start()
}
