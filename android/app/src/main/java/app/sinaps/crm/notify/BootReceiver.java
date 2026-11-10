package app.sinaps.crm.notify;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/** После перезагрузки телефона и обновления приложения связь с CRM поднимается сама. */
public class BootReceiver extends BroadcastReceiver {
    @Override public void onReceive(Context c, Intent i) {
        if (Cfg.paired(c)) LinkService.start(c);
    }
}
