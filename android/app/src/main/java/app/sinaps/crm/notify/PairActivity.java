package app.sinaps.crm.notify;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.view.Gravity;
import android.view.ViewGroup;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import com.google.zxing.integration.android.IntentIntegrator;
import com.google.zxing.integration.android.IntentResult;

import org.json.JSONObject;

/** Привязка телефона к CRM: QR-код с экрана компьютера или адрес и код вручную. */
public class PairActivity extends Activity {
    private final Handler main = new Handler(Looper.getMainLooper());
    private TextView note, scan, connect;
    private EditText addr, code;
    private boolean busy;

    @Override protected void onCreate(Bundle b) {
        super.onCreate(b);
        LinearLayout root = Ui.col(this);
        root.setPadding(Ui.dp(this, 20), Ui.dp(this, 28), Ui.dp(this, 20), Ui.dp(this, 20));

        LinearLayout brand = Ui.row(this);
        TextView mark = Ui.text(this, "S", 20, Ui.INK, true);
        mark.setGravity(Gravity.CENTER);
        mark.setBackground(Ui.box(this, Ui.ACCENT, 0, 12));
        brand.addView(mark, new LinearLayout.LayoutParams(Ui.dp(this, 44), Ui.dp(this, 44)));
        LinearLayout bn = Ui.col(this);
        bn.setPadding(Ui.dp(this, 12), 0, 0, 0);
        bn.addView(Ui.text(this, "SINAPS", 17, Ui.TEXT, true));
        bn.addView(Ui.text(this, "уведомления из CRM", 12, Ui.MUTED, false));
        brand.addView(bn);
        root.addView(brand);

        root.addView(Ui.text(this, "Привяжи телефон к своей CRM", 24, Ui.TEXT, true), top(28));
        root.addView(Ui.text(this, "На компьютере открой «Плагины → Уведомления → Телефон», нажми «Привязать телефон» и наведи камеру на QR-код.", 15, 0xFFC9CCD2, false), top(10));

        scan = Ui.button(this, "Сканировать QR-код", 0);
        scan.setOnClickListener(v -> scan());
        root.addView(scan, top(24));

        root.addView(Ui.text(this, "Или введи вручную — адрес и код показаны рядом с QR-кодом", 13, Ui.MUTED, false), top(28));
        addr = field("Адрес, например 192.168.1.42:47815", InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        root.addView(addr, top(10));
        code = field("Код из 8 цифр", InputType.TYPE_CLASS_NUMBER);
        root.addView(code, top(8));
        connect = Ui.button(this, "Привязать по коду", 1);
        connect.setOnClickListener(v -> manual());
        root.addView(connect, top(10));

        note = Ui.text(this, "", 14, Ui.DANGER, false);
        root.addView(note, top(16));
        root.addView(Ui.text(this, "Телефон и компьютер должны быть в одной сети Wi-Fi. Приложение только показывает сообщения — клиентам оно ничего не отправляет.", 12, Ui.MUTED, false), top(18));

        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(Ui.BG);
        scroll.setFillViewport(true);
        scroll.addView(root);
        setContentView(scroll);

        handleLink(getIntent());
    }

    @Override protected void onNewIntent(Intent i) {
        super.onNewIntent(i);
        handleLink(i);
    }

    private LinearLayout.LayoutParams top(float dp) {
        return Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), dp);
    }

    private EditText field(String hint, int type) {
        EditText e = new EditText(this);
        e.setHint(hint);
        e.setHintTextColor(Ui.MUTED);
        e.setTextColor(Ui.TEXT);
        e.setTextSize(16);
        e.setSingleLine(true);
        e.setInputType(type);
        e.setMinHeight(Ui.dp(this, 52));
        e.setPadding(Ui.dp(this, 16), 0, Ui.dp(this, 16), 0);
        e.setBackground(Ui.box(this, Ui.CARD, Ui.LINE, 14));
        return e;
    }

    // ---------- откуда берём адрес и код ----------

    /** QR-код отсканировали обычной камерой телефона: sinaps://pair?h=…&p=…&c=… */
    private void handleLink(Intent i) {
        Uri u = i == null ? null : i.getData();
        if (u != null && "sinaps".equals(u.getScheme())) fromUri(u);
    }

    private void scan() {
        IntentIntegrator ii = new IntentIntegrator(this);
        ii.setDesiredBarcodeFormats(IntentIntegrator.QR_CODE);
        ii.setPrompt("Наведи камеру на QR-код в CRM");
        ii.setBeepEnabled(false);
        ii.setOrientationLocked(true);
        ii.initiateScan();
    }

    @Override protected void onActivityResult(int req, int res, Intent data) {
        IntentResult r = IntentIntegrator.parseActivityResult(req, res, data);
        if (r == null) { super.onActivityResult(req, res, data); return; }
        if (r.getContents() == null) return; // сканирование отменили
        Uri u = Uri.parse(r.getContents().trim());
        if (!"sinaps".equals(u.getScheme())) { fail("Это не QR-код из SINAPS CRM"); return; }
        fromUri(u);
    }

    private void fromUri(Uri u) {
        String h = u.getQueryParameter("h"), p = u.getQueryParameter("p"), c = u.getQueryParameter("c");
        int port = Cfg.DEFAULT_PORT;
        try { if (p != null) port = Integer.parseInt(p); } catch (NumberFormatException ignored) {}
        if (h == null || h.isEmpty() || c == null || c.isEmpty()) { fail("В QR-коде нет адреса или кода — обнови код в CRM"); return; }
        pair(h, port, c);
    }

    private void manual() {
        String a = addr.getText().toString().trim().replace(" ", "");
        String c = code.getText().toString().replaceAll("\\D", "");
        int port = Cfg.DEFAULT_PORT;
        int colon = a.lastIndexOf(':');
        if (colon > 0 && a.indexOf(':') == colon) { // IPv4 или имя с портом
            try { port = Integer.parseInt(a.substring(colon + 1)); } catch (NumberFormatException e) { fail("Порт после двоеточия должен быть числом"); return; }
            a = a.substring(0, colon);
        }
        if (a.isEmpty()) { fail("Введи адрес компьютера — он показан в CRM рядом с QR-кодом"); return; }
        if (c.length() != 8) { fail("Код — это 8 цифр с экрана CRM"); return; }
        pair(a, port, c);
    }

    // ---------- привязка ----------

    private void pair(final String hosts, final int port, final String pin) {
        if (busy) return;
        setBusy(true);
        note.setTextColor(Ui.MUTED);
        note.setText("Подключаемся к CRM…");
        new Thread(() -> {
            String err = "Не получилось подключиться к компьютеру. Проверь, что CRM открыта, а телефон в той же сети Wi-Fi.";
            try {
                byte[] key = Proto.pairKey(pin);
                for (String host : hosts.split(",")) {
                    host = host.trim();
                    if (host.isEmpty()) continue;
                    try (Proto.Link l = Proto.Link.open(host, port, "SINAPS1 PAIR", key, 6000)) {
                        l.send(new JSONObject().put("t", "pair").put("name", LinkService.deviceName()));
                        JSONObject f = l.recv();
                        if (!"paired".equals(f.optString("t"))) continue;
                        // этот адрес сработал — ставим его первым, остальные оставляем запасными
                        StringBuilder order = new StringBuilder(host);
                        for (String other : hosts.split(",")) {
                            if (!other.trim().isEmpty() && !other.trim().equals(host)) order.append(',').append(other.trim());
                        }
                        Cfg.savePairing(this, order.toString(), port, f.getString("id"), f.getString("key"), f.optString("crm", "SINAPS CRM"));
                        main.post(this::done);
                        return;
                    } catch (java.net.SocketTimeoutException | java.net.ConnectException | java.net.NoRouteToHostException | java.net.UnknownHostException e) {
                        // этот адрес недоступен — пробуем следующий
                    } catch (Exception e) {
                        // подключились, но CRM не ответила на наш кадр: код неверный, истёк или уже использован
                        err = "CRM не приняла код. Он действует 5 минут и срабатывает один раз — покажи новый код в CRM.";
                    }
                }
            } catch (Exception e) {
                err = "Ошибка привязки: " + e.getMessage();
            }
            final String msg = err;
            main.post(() -> fail(msg));
        }, "sinaps-pair").start();
    }

    private void done() {
        if (isFinishing()) return;
        startActivity(new Intent(this, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK));
        finish();
    }

    private void fail(String msg) {
        setBusy(false);
        note.setTextColor(Ui.DANGER);
        note.setText(msg);
    }

    private void setBusy(boolean b) {
        busy = b;
        scan.setEnabled(!b);
        connect.setEnabled(!b);
        scan.setAlpha(b ? .5f : 1f);
        connect.setAlpha(b ? .5f : 1f);
    }
}
