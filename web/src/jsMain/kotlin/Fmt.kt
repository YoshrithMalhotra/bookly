import kotlin.js.Date

/** Date helpers. Times are always shown in the business's timezone, not the viewer's. */
object Fmt {
    private fun opts(vararg pairs: Pair<String, Any?>): dynamic {
        val o: dynamic = js("({})")
        for ((k, v) in pairs) o[k] = v
        return o
    }

    fun time(iso: String, tz: String): String =
        Date(iso).asDynamic().toLocaleTimeString(undefined, opts("hour" to "2-digit", "minute" to "2-digit", "timeZone" to tz)) as String

    fun dateLong(iso: String, tz: String): String =
        Date(iso).asDynamic().toLocaleDateString(
            undefined,
            opts("weekday" to "long", "day" to "numeric", "month" to "long", "timeZone" to tz),
        ) as String

    /** Heading for a YYYY-MM-DD date, e.g. "Monday 2 November". */
    fun day(date: String): String =
        Date(date + "T12:00:00Z").asDynamic().toLocaleDateString(
            undefined,
            opts("weekday" to "long", "day" to "numeric", "month" to "long", "timeZone" to "UTC"),
        ) as String

    /** Today's date (YYYY-MM-DD) in a timezone. */
    fun today(tz: String): String =
        Date().asDynamic().toLocaleDateString("en-CA", opts("timeZone" to tz)) as String

    fun addDays(date: String, n: Int): String {
        val d = Date(date + "T12:00:00Z")
        val shifted = Date(d.getTime() + n * 86_400_000.0)
        return (shifted.toISOString()).substring(0, 10)
    }

    /** 0 = Sunday, matching the API. */
    fun weekday(date: String): Int = Date(date + "T12:00:00Z").getUTCDay()

    fun browserTimezone(): String =
        js("Intl.DateTimeFormat().resolvedOptions().timeZone") as String? ?: "UTC"

    fun timezones(): List<String> {
        val list = js("typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : []")
            .unsafeCast<Array<String>>().toMutableList()
        val mine = browserTimezone()
        if (mine !in list) list.add(0, mine)
        return list
    }

    val weekdayNames = listOf("Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday")

    fun duration(min: Int): String = when {
        min < 60 -> "$min min"
        min % 60 == 0 -> "${min / 60} h"
        else -> "${min / 60} h ${min % 60} min"
    }

    /** "25.00" in GBP -> "£25.00", using the viewer's locale. */
    fun money(amount: String, currency: String): String {
        val n = amount.toDoubleOrNull() ?: return amount
        return try {
            val nf = js("Intl.NumberFormat")(undefined, opts("style" to "currency", "currency" to currency))
            nf.format(n) as String
        } catch (e: Throwable) {
            "$amount $currency"
        }
    }

    /** 24-hour "HH:mm" of an instant in tz, for time inputs. */
    fun clock(iso: String, tz: String): String {
        val f = js("Intl.DateTimeFormat")("en-GB", opts("hour" to "2-digit", "minute" to "2-digit", "hourCycle" to "h23", "timeZone" to tz))
        return f.format(Date(iso)) as String
    }

    /** YYYY-MM-DD of an instant in tz. */
    fun dateIn(iso: String, tz: String): String =
        Date(iso).asDynamic().toLocaleDateString("en-CA", opts("timeZone" to tz)) as String

    /** Milliseconds as if tz's wall clock at instant ms were UTC. */
    private fun wallAsUtc(ms: Double, tz: String): Double {
        val f = js("Intl.DateTimeFormat")("en-US", opts(
            "timeZone" to tz, "hourCycle" to "h23", "year" to "numeric", "month" to "2-digit",
            "day" to "2-digit", "hour" to "2-digit", "minute" to "2-digit", "second" to "2-digit",
        ))
        val parts = f.formatToParts(Date(ms)).unsafeCast<Array<dynamic>>()
        fun part(type: String): Int = (parts.first { it.type == type }.value as String).toInt()
        return js("Date.UTC")(part("year"), part("month") - 1, part("day"), part("hour") % 24, part("minute"), part("second")) as Double
    }

    /** The instant (ISO, UTC) when tz's clocks show date + time ("2026-11-02", "09:30"). */
    fun zonedToIso(date: String, time: String, tz: String): String {
        val (y, m, d) = date.split("-").map { it.toInt() }
        val (h, mi) = time.split(":").map { it.toInt() }
        val target = js("Date.UTC")(y, m - 1, d, h, mi) as Double
        var guess = target
        repeat(3) { guess += target - wallAsUtc(guess, tz) }
        return Date(guess).toISOString()
    }

    val currencies = listOf("GBP", "EUR", "USD", "INR", "AUD", "CAD", "NZD", "AED", "SGD", "ZAR", "CHF", "SEK", "NOK", "DKK", "PLN", "JPY")
}
