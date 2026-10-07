import kotlinx.browser.window
import kotlinx.coroutines.await
import kotlinx.serialization.json.Json
import org.w3c.fetch.RequestInit
import org.w3c.fetch.SAME_ORIGIN
import org.w3c.fetch.RequestCredentials

class ApiException(val status: Int, message: String) : Exception(message)

/** Thin JSON client for the Go API. All errors come back as {"error": "..."}. */
object Api {
    val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    suspend fun request(method: String, path: String, body: String? = null): String {
        val headers = js("({})")
        headers["Accept"] = "application/json"
        if (body != null) headers["Content-Type"] = "application/json"
        val init = RequestInit(
            method = method,
            headers = headers,
            body = body ?: undefined,
            credentials = RequestCredentials.SAME_ORIGIN,
        )
        val resp = try {
            window.fetch(path, init).await()
        } catch (e: Throwable) {
            throw ApiException(0, "Can't reach the server. Check your connection and try again.")
        }
        val text = resp.text().await()
        if (!resp.ok) {
            val msg = try {
                json.decodeFromString<ApiError>(text).error
            } catch (e: Throwable) {
                "Something went wrong (HTTP ${resp.status})"
            }
            throw ApiException(resp.status.toInt(), msg.replaceFirstChar { it.uppercase() })
        }
        return text
    }

    suspend inline fun <reified T> get(path: String): T = json.decodeFromString(request("GET", path))

    suspend inline fun <reified B, reified T> send(method: String, path: String, body: B): T =
        json.decodeFromString(request(method, path, json.encodeToString(body)))

    suspend fun post(path: String) {
        request("POST", path)
    }
}
