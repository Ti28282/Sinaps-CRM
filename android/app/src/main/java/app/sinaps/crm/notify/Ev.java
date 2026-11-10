package app.sinaps.crm.notify;

/** Событие из CRM, как оно лежит в ленте. */
final class Ev {
    long seq;
    String kind = "";   // msg | fresh | reply | silent | wait | alarm | digest | test
    String title = "";
    String text = "";
    long chatId;
    String client = "";
    String channel = ""; // tg | wa
    String username = "";
    String phone = "";
    String stage = "";
    String msg = "";
    long at;
    boolean read;

    /** Событие — сообщение клиента (а не напоминание CRM). */
    boolean isChat() { return "msg".equals(kind) || "fresh".equals(kind) || "reply".equals(kind); }
}
