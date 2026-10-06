import kotlinx.coroutines.launch
import kotlinx.html.*
import kotlinx.html.js.onInputFunction
import kotlinx.html.js.onSubmitFunction
import org.w3c.dom.HTMLElement
import org.w3c.dom.HTMLInputElement

class LoginPage(private val root: HTMLElement) {
    private var error: String? = null
    private var busy = false

    fun show() {
        setTitle("Log in")
        // Already logged in? Go straight to the dashboard.
        scope.launch {
            try {
                Api.get<Business>("/api/owner/business")
                Router.go("/owner", replace = true)
            } catch (e: Throwable) {
                render()
            }
        }
    }

    private fun render() {
        root.render {
            div("page") {
                topBar { link("/signup", "button ghost", "Sign up") }
                main("container narrow") {
                    div("card") {
                        h1 { +"Log in" }
                        errorBox(error)
                        form {
                            onSubmitFunction = { e -> e.stop(); submit() }
                            label {
                                +"Email"
                                input(InputType.email) { id = "email"; required = true; attributes["autocomplete"] = "email" }
                            }
                            label {
                                +"Password"
                                input(InputType.password) { id = "password"; required = true; attributes["autocomplete"] = "current-password" }
                            }
                            button(type = ButtonType.submit, classes = "button full") {
                                disabled = busy
                                +(if (busy) "Logging in…" else "Log in")
                            }
                        }
                        p("muted") { +"New to Bookly? "; link("/signup", null, "Create an account") }
                    }
                }
            }
        }
    }

    private fun submit() {
        val email = inputValue("email")
        val password = rawInputValue("password")
        busy = true; error = null; render()
        scope.launch {
            try {
                Api.send<LoginRequest, Business>("POST", "/api/login", LoginRequest(email, password))
                Router.go("/owner")
            } catch (e: ApiException) {
                busy = false; error = e.message; render()
                (kotlinx.browser.document.getElementById("email") as? HTMLInputElement)?.value = email
            }
        }
    }
}

class SignupPage(private val root: HTMLElement) {
    private var error: String? = null
    private var busy = false
    private var slugEdited = false
    private var draft = SignupRequest("", "", Fmt.browserTimezone(), "", "")

    fun show() {
        setTitle("Sign up")
        render()
    }

    private fun slugify(s: String): String =
        s.lowercase().replace(Regex("[^a-z0-9]+"), "-").trim('-').take(50)

    private fun render() {
        val origin = kotlinx.browser.window.location.origin
        root.render {
            div("page") {
                topBar { link("/login", "button ghost", "Log in") }
                main("container narrow") {
                    div("card") {
                        h1 { +"Create your booking page" }
                        p("muted") { +"Takes a minute. You can change everything later." }
                        errorBox(error)
                        form {
                            onSubmitFunction = { e -> e.stop(); submit() }
                            label {
                                +"Business name"
                                input(InputType.text) {
                                    id = "business_name"; required = true; maxLength = "100"; value = draft.businessName
                                    attributes["autocomplete"] = "organization"
                                    onInputFunction = {
                                        if (!slugEdited) {
                                            (kotlinx.browser.document.getElementById("slug") as HTMLInputElement).value =
                                                slugify(inputValue("business_name"))
                                        }
                                    }
                                }
                            }
                            label {
                                +"Booking link"
                                div("input-prefix") {
                                    span { +"$origin/b/" }
                                    input(InputType.text) {
                                        id = "slug"; required = true; value = draft.slug
                                        pattern = "[a-z0-9][a-z0-9\\-]{1,48}[a-z0-9]"
                                        title = "3–50 lowercase letters, numbers or dashes"
                                        onInputFunction = { slugEdited = true }
                                    }
                                }
                            }
                            label {
                                +"Timezone"
                                select {
                                    id = "timezone"
                                    for (tz in Fmt.timezones()) option { value = tz; selected = tz == draft.timezone; +tz }
                                }
                            }
                            label {
                                +"Email"
                                input(InputType.email) { id = "email"; required = true; value = draft.email; attributes["autocomplete"] = "email" }
                            }
                            label {
                                +"Password"
                                input(InputType.password) {
                                    id = "password"; required = true; minLength = "10"; maxLength = "72"
                                    attributes["autocomplete"] = "new-password"
                                }
                                small("muted") { +"At least 10 characters." }
                            }
                            button(type = ButtonType.submit, classes = "button full") {
                                disabled = busy
                                +(if (busy) "Creating…" else "Create account")
                            }
                        }
                    }
                }
            }
        }
    }

    private fun submit() {
        draft = SignupRequest(
            businessName = inputValue("business_name"),
            slug = inputValue("slug").lowercase(),
            timezone = selectValue("timezone"),
            email = inputValue("email"),
            password = rawInputValue("password"),
        )
        busy = true; error = null; render()
        scope.launch {
            try {
                Api.send<SignupRequest, Business>("POST", "/api/signup", draft)
                Router.go("/owner/services")
            } catch (e: ApiException) {
                busy = false; error = e.message; render()
            }
        }
    }
}
