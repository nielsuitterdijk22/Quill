UPDATE tenants SET plan = 'free' WHERE plan IN ('trial', 'lapsed');
DROP TABLE IF EXISTS tenant_subscriptions;
